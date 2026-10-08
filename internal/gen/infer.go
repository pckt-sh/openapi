package gen

import (
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/go-faster/yaml"

	"github.com/pckt-sh/openapi/internal/openapi"
)

// typeOrder is the order of types in an inferred type list.
var typeOrder = []string{"object", "array", "string", "integer", "number", "boolean", "null"}

// maxExampleLength is the length above which a string value is not kept as
// a property example, to keep documents small.
const maxExampleLength = 256

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// shape accumulates the values seen at one location of an example: every
// value of a field across array items is added to the same shape, so a list
// of results describes the field better than a single object.
type shape struct {
	types map[string]bool

	// object properties, in the order they first appear
	props     []string
	propShape map[string]*shape

	// array items, nil while no item has been seen (empty arrays)
	items *shape

	// format shared by every string value, "" when none or inconsistent
	format      string
	seenStrings bool

	// example is the first non-null scalar value seen
	example *yaml.Node
}

func newShape() *shape {
	return &shape{types: map[string]bool{}, propShape: map[string]*shape{}}
}

// inferSchema returns a schema describing an example value. No property is
// required: a sample cannot prove a field is always present. Scalar schemas
// get the first value seen as example; objects and arrays get none, their
// properties and items carry them.
func inferSchema(n *yaml.Node) *openapi.Schema {
	s := newShape()
	s.add(n)
	return s.schema()
}

func (s *shape) add(n *yaml.Node) {
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) > 0 {
			s.add(n.Content[0])
		}
	case yaml.AliasNode:
		s.add(n.Alias)
	case yaml.MappingNode:
		s.types["object"] = true
		for i := 0; i+1 < len(n.Content); i += 2 {
			key := n.Content[i].Value
			ps, ok := s.propShape[key]
			if !ok {
				ps = newShape()
				s.propShape[key] = ps
				s.props = append(s.props, key)
			}
			ps.add(n.Content[i+1])
		}
	case yaml.SequenceNode:
		s.types["array"] = true
		for _, c := range n.Content {
			if s.items == nil {
				s.items = newShape()
			}
			s.items.add(c)
		}
	case yaml.ScalarNode:
		example := n
		switch n.ShortTag() {
		case "!!int":
			s.types["integer"] = true
		case "!!float":
			s.types["number"] = true
		case "!!bool":
			s.types["boolean"] = true
		case "!!null":
			s.types["null"] = true
		default: // !!str, and YAML !!timestamp or !!binary values
			s.types["string"] = true
			s.addString(n.Value)
			// keep the text as written, a YAML timestamp would be re-encoded
			example = &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: n.Value}
		}
		if s.example == nil && n.ShortTag() != "!!null" && len(n.Value) <= maxExampleLength {
			s.example = example
		}
	}
}

func (s *shape) addString(v string) {
	f := detectFormat(v)
	if !s.seenStrings {
		s.format, s.seenStrings = f, true
	} else if s.format != f {
		s.format = ""
	}
}

func (s *shape) schema() *openapi.Schema {
	out := &openapi.Schema{}

	types := s.types
	if types["integer"] && types["number"] {
		// a field holding 1 and 1.5 is a number
		delete(types, "integer")
	}
	for _, t := range typeOrder {
		if types[t] {
			out.Type = append(out.Type, t)
		}
	}

	if types["object"] {
		for _, name := range s.props {
			out.Properties = append(out.Properties, openapi.Property{Name: name, Schema: s.propShape[name].schema()})
		}
	}
	if types["array"] {
		if s.items != nil {
			out.Items = s.items.schema()
		} else {
			out.Items = &openapi.Schema{}
		}
	}
	if types["string"] {
		out.Format = s.format
	}
	if s.example != nil {
		out.Examples = []any{openapi.Node{Node: s.example}}
	}
	return out
}

// detectFormat returns the JSON Schema format of a string value, if it matches
// a common one unambiguously.
func detectFormat(v string) string {
	switch {
	case uuidRe.MatchString(v):
		return "uuid"
	case isTime(time.RFC3339Nano, v):
		return "date-time"
	case isTime(time.DateOnly, v):
		return "date"
	case isEmail(v):
		return "email"
	case isURI(v):
		return "uri"
	}
	return ""
}

func isTime(layout, v string) bool {
	_, err := time.Parse(layout, v)
	return err == nil
}

func isEmail(v string) bool {
	if strings.ContainsAny(v, " <>") {
		return false
	}
	addr, err := mail.ParseAddress(v)
	return err == nil && addr.Address == v
}

func isURI(v string) bool {
	u, err := url.Parse(v)
	return err == nil && u.Scheme != "" && u.Host != ""
}
