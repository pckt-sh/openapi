// Package merge combines several OpenAPI documents into one.
//
// Documents are handled as YAML nodes so that key order and every field,
// including ones unknown to this package (security, x-* extensions...), are
// preserved. JSON documents are read as YAML, which is a superset of JSON.
package merge

import (
	"bytes"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"

	"github.com/go-faster/yaml"
)

// DefaultVersion is the OpenAPI version used when no input declares one.
const DefaultVersion = "3.1.0"

// Input is a named OpenAPI document.
type Input struct {
	Name string
	Data []byte
}

// Options configure the merged document.
type Options struct {
	// Base is an optional document merged first, providing info, servers,
	// security and any other top-level fields.
	Base *Input
	// Title, Version and Description override info fields when not empty.
	Title       string
	Version     string
	Description string
	// Servers are appended to the servers of the base document.
	Servers []string
	// Warnings receives non-fatal conflicts, discarded when nil.
	Warnings io.Writer
}

// topLevelOrder is the order of well-known top-level keys in the output.
var topLevelOrder = []string{"openapi", "info", "jsonSchemaDialect", "servers", "paths", "webhooks", "components", "security", "tags", "externalDocs"}

type merger struct {
	opts Options
	out  *yaml.Node
	// owners maps a location (`paths./v1/x.get`) to the input defining it.
	owners map[string]string
}

// Merge combines the inputs in order. Identical definitions are kept once; a
// path operation, component or top-level field defined differently by two
// inputs is an error.
func Merge(inputs []Input, opts Options) (*yaml.Node, error) {
	m := &merger{
		opts:   opts,
		out:    &yaml.Node{Kind: yaml.MappingNode},
		owners: map[string]string{},
	}

	if opts.Base != nil {
		if err := m.mergeDoc(*opts.Base, true); err != nil {
			return nil, err
		}
	}
	for _, in := range inputs {
		if err := m.mergeDoc(in, false); err != nil {
			return nil, err
		}
	}

	m.applyOptions()
	if err := m.checkOperationIDs(); err != nil {
		return nil, err
	}
	m.sortTopLevel()
	sortKeys(get(m.out, "paths"))
	sortKeys(get(m.out, "webhooks"))
	if components := get(m.out, "components"); components != nil {
		for i := 1; i < len(components.Content); i += 2 {
			sortKeys(components.Content[i])
		}
	}

	return m.out, nil
}

func (m *merger) warnf(format string, args ...any) {
	if m.opts.Warnings != nil {
		fmt.Fprintf(m.opts.Warnings, "warning: "+format+"\n", args...)
	}
}

// Load parses a YAML or JSON OpenAPI document and returns its root mapping.
func Load(in Input) (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(in.Data, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", in.Name, err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s: document is not a mapping", in.Name)
	}
	root := doc.Content[0]
	normalizeStyle(root)
	return root, nil
}

func (m *merger) mergeDoc(in Input, isBase bool) error {
	root, err := Load(in)
	if err != nil {
		return err
	}

	for i := 0; i < len(root.Content); i += 2 {
		key, val := root.Content[i].Value, root.Content[i+1]
		switch key {
		case "openapi":
			if cur := get(m.out, key); cur == nil {
				set(m.out, key, val)
			} else if cur.Value != val.Value {
				m.warnf("%s: openapi version %s differs from %s, keeping %s", in.Name, val.Value, cur.Value, cur.Value)
			}
		case "info":
			// Per-file documents carry generated info, only the base one is kept.
			if isBase {
				set(m.out, key, val)
			}
		case "paths", "webhooks":
			if err := m.mergePaths(in.Name, key, val); err != nil {
				return err
			}
		case "components":
			if err := m.mergeComponents(in.Name, val); err != nil {
				return err
			}
		case "tags":
			m.mergeTags(in.Name, val)
		case "servers":
			m.mergeServers(val)
		default:
			if err := m.mergeEntry(in.Name, m.out, key, val, key); err != nil {
				return err
			}
		}
	}
	return nil
}

// mergeEntry adds key to parent, or checks the existing value is identical.
func (m *merger) mergeEntry(source string, parent *yaml.Node, key string, val *yaml.Node, loc string) error {
	cur := get(parent, key)
	if cur == nil {
		set(parent, key, val)
		m.owners[loc] = source
		return nil
	}
	if !equal(cur, val) {
		return fmt.Errorf("conflict on %s: defined differently in %s and %s", loc, m.owners[loc], source)
	}
	return nil
}

func (m *merger) mergePaths(source, section string, paths *yaml.Node) error {
	if paths.Kind != yaml.MappingNode {
		return fmt.Errorf("%s: %s is not a mapping", source, section)
	}
	out := get(m.out, section)
	if out == nil {
		out = &yaml.Node{Kind: yaml.MappingNode}
		set(m.out, section, out)
	}

	for i := 0; i < len(paths.Content); i += 2 {
		path, item := paths.Content[i].Value, paths.Content[i+1]
		curItem := get(out, path)
		if curItem == nil {
			curItem = &yaml.Node{Kind: yaml.MappingNode}
			set(out, path, curItem)
		}
		if item.Kind != yaml.MappingNode {
			return fmt.Errorf("%s: %s %q is not a mapping", source, section, path)
		}
		for j := 0; j < len(item.Content); j += 2 {
			key := item.Content[j].Value
			if err := m.mergeEntry(source, curItem, key, item.Content[j+1], section+"."+path+"."+key); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *merger) mergeComponents(source string, components *yaml.Node) error {
	if components.Kind != yaml.MappingNode {
		return fmt.Errorf("%s: components is not a mapping", source)
	}
	out := get(m.out, "components")
	if out == nil {
		out = &yaml.Node{Kind: yaml.MappingNode}
		set(m.out, "components", out)
	}

	for i := 0; i < len(components.Content); i += 2 {
		kind, entries := components.Content[i].Value, components.Content[i+1]
		if entries.Kind != yaml.MappingNode {
			// Extensions (x-*) and other scalar fields.
			if err := m.mergeEntry(source, out, kind, entries, "components."+kind); err != nil {
				return err
			}
			continue
		}
		curKind := get(out, kind)
		if curKind == nil {
			curKind = &yaml.Node{Kind: yaml.MappingNode}
			set(out, kind, curKind)
		}
		for j := 0; j < len(entries.Content); j += 2 {
			name := entries.Content[j].Value
			if err := m.mergeEntry(source, curKind, name, entries.Content[j+1], "components."+kind+"."+name); err != nil {
				return err
			}
		}
	}
	return nil
}

// mergeTags unions tags by name. The first non-empty description wins.
func (m *merger) mergeTags(source string, tags *yaml.Node) {
	out := get(m.out, "tags")
	if out == nil {
		out = &yaml.Node{Kind: yaml.SequenceNode}
		set(m.out, "tags", out)
	}

	for _, tag := range tags.Content {
		name := get(tag, "name")
		if name == nil {
			continue
		}
		idx := slices.IndexFunc(out.Content, func(t *yaml.Node) bool {
			n := get(t, "name")
			return n != nil && n.Value == name.Value
		})
		if idx < 0 {
			out.Content = append(out.Content, tag)
			continue
		}

		cur := out.Content[idx]
		curDesc, newDesc := get(cur, "description"), get(tag, "description")
		switch {
		case newDesc == nil || newDesc.Value == "":
		case curDesc == nil || curDesc.Value == "":
			out.Content[idx] = tag
		case curDesc.Value != newDesc.Value:
			m.warnf("%s: tag %q has a different description, keeping the first one", source, name.Value)
		}
	}
}

func (m *merger) mergeServers(servers *yaml.Node) {
	out := get(m.out, "servers")
	if out == nil {
		out = &yaml.Node{Kind: yaml.SequenceNode}
		set(m.out, "servers", out)
	}
	for _, srv := range servers.Content {
		if !slices.ContainsFunc(out.Content, func(n *yaml.Node) bool { return equal(n, srv) }) {
			out.Content = append(out.Content, srv)
		}
	}
}

func (m *merger) applyOptions() {
	if get(m.out, "openapi") == nil {
		set(m.out, "openapi", str(DefaultVersion))
	}

	info := get(m.out, "info")
	if info == nil {
		info = &yaml.Node{Kind: yaml.MappingNode}
		set(m.out, "info", info)
	}
	if m.opts.Title != "" || get(info, "title") == nil {
		title := m.opts.Title
		if title == "" {
			title = "API"
		}
		set(info, "title", str(title))
	}
	if m.opts.Description != "" {
		set(info, "description", str(m.opts.Description))
	}
	if m.opts.Version != "" || get(info, "version") == nil {
		version := m.opts.Version
		if version == "" {
			version = "0.0.0"
		}
		set(info, "version", str(version))
	}

	if len(m.opts.Servers) > 0 {
		servers := &yaml.Node{Kind: yaml.SequenceNode}
		for _, url := range m.opts.Servers {
			srv := &yaml.Node{Kind: yaml.MappingNode}
			set(srv, "url", str(url))
			servers.Content = append(servers.Content, srv)
		}
		m.mergeServers(servers)
	}
}

var httpMethods = []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"}

// checkOperationIDs ensures operation IDs are unique in the merged document.
func (m *merger) checkOperationIDs() error {
	seen := map[string]string{}
	for _, section := range []string{"paths", "webhooks"} {
		paths := get(m.out, section)
		if paths == nil {
			continue
		}
		for i := 0; i < len(paths.Content); i += 2 {
			path, item := paths.Content[i].Value, paths.Content[i+1]
			for j := 0; j < len(item.Content); j += 2 {
				method := item.Content[j].Value
				if !slices.Contains(httpMethods, method) {
					continue
				}
				id := get(item.Content[j+1], "operationId")
				if id == nil {
					continue
				}
				loc := section + "." + path + "." + method
				if prev, ok := seen[id.Value]; ok {
					return fmt.Errorf("duplicate operationId %q: %s (%s) and %s (%s)", id.Value, prev, m.owners[prev], loc, m.owners[loc])
				}
				seen[id.Value] = loc
			}
		}
	}
	return nil
}

// sortTopLevel orders well-known top-level keys, keeping others after them.
func (m *merger) sortTopLevel() {
	rank := func(key string) int {
		if i := slices.Index(topLevelOrder, key); i >= 0 {
			return i
		}
		return len(topLevelOrder)
	}
	sortMapping(m.out, func(a, b string) int { return rank(a) - rank(b) })
}

// sortKeys orders a mapping by key, so the output does not depend on the order of inputs.
func sortKeys(n *yaml.Node) {
	if n != nil && n.Kind == yaml.MappingNode {
		sortMapping(n, strings.Compare)
	}
}

func sortMapping(n *yaml.Node, cmp func(a, b string) int) {
	type pair struct{ k, v *yaml.Node }
	pairs := make([]pair, 0, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		pairs = append(pairs, pair{n.Content[i], n.Content[i+1]})
	}
	slices.SortStableFunc(pairs, func(a, b pair) int { return cmp(a.k.Value, b.k.Value) })

	n.Content = n.Content[:0]
	for _, p := range pairs {
		n.Content = append(n.Content, p.k, p.v)
	}
}

// EncodeYAML writes the document as YAML.
func EncodeYAML(root *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(root); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func get(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

func set(mapping *yaml.Node, key string, val *yaml.Node) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content[i+1] = val
			return
		}
	}
	mapping.Content = append(mapping.Content, str(key), val)
}

func str(v string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
}

// equal compares two nodes by value, ignoring style and key positions.
func equal(a, b *yaml.Node) bool {
	var va, vb any
	if a.Decode(&va) != nil || b.Decode(&vb) != nil {
		return false
	}
	return reflect.DeepEqual(va, vb)
}

// normalizeStyle drops flow and quoting styles (JSON inputs parse as flow
// YAML), letting the encoder pick block style and quote only when needed.
func normalizeStyle(n *yaml.Node) {
	n.Style &^= yaml.FlowStyle | yaml.DoubleQuotedStyle | yaml.SingleQuotedStyle
	n.Line, n.Column = 0, 0
	for _, c := range n.Content {
		normalizeStyle(c)
	}
}
