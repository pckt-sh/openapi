// Package openapi contains the subset of the OpenAPI 3.1 document model
// emitted by protoc-gen-pckt-openapi.
package openapi

import (
	"bytes"
	"encoding/json"
	"maps"
	"slices"

	"github.com/go-faster/yaml"
)

// Version is the OpenAPI version written in generated documents.
const Version = "3.1.0"

type Document struct {
	OpenAPI    string      `json:"openapi" yaml:"openapi"`
	Info       Info        `json:"info" yaml:"info"`
	Servers    []Server    `json:"servers,omitempty" yaml:"servers,omitempty"`
	Paths      Paths       `json:"paths,omitempty" yaml:"paths,omitempty"`
	Components *Components `json:"components,omitempty" yaml:"components,omitempty"`
	Tags       []Tag       `json:"tags,omitempty" yaml:"tags,omitempty"`
}

type Info struct {
	Title       string `json:"title" yaml:"title"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	Version     string `json:"version" yaml:"version"`
}

type Server struct {
	URL         string `json:"url" yaml:"url"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

type Paths map[string]*PathItem

type PathItem struct {
	Get     *Operation `json:"get,omitempty" yaml:"get,omitempty"`
	Put     *Operation `json:"put,omitempty" yaml:"put,omitempty"`
	Post    *Operation `json:"post,omitempty" yaml:"post,omitempty"`
	Delete  *Operation `json:"delete,omitempty" yaml:"delete,omitempty"`
	Options *Operation `json:"options,omitempty" yaml:"options,omitempty"`
	Head    *Operation `json:"head,omitempty" yaml:"head,omitempty"`
	Patch   *Operation `json:"patch,omitempty" yaml:"patch,omitempty"`
	Trace   *Operation `json:"trace,omitempty" yaml:"trace,omitempty"`
}

// Operation returns a pointer to the operation slot for the given lowercase
// HTTP method, or nil when the method is not supported by OpenAPI.
func (p *PathItem) Operation(method string) **Operation {
	switch method {
	case "get":
		return &p.Get
	case "put":
		return &p.Put
	case "post":
		return &p.Post
	case "delete":
		return &p.Delete
	case "options":
		return &p.Options
	case "head":
		return &p.Head
	case "patch":
		return &p.Patch
	case "trace":
		return &p.Trace
	}
	return nil
}

type Operation struct {
	Tags        []string             `json:"tags,omitempty" yaml:"tags,omitempty"`
	Summary     string               `json:"summary,omitempty" yaml:"summary,omitempty"`
	Description string               `json:"description,omitempty" yaml:"description,omitempty"`
	OperationID string               `json:"operationId,omitempty" yaml:"operationId,omitempty"`
	Parameters  []*Parameter         `json:"parameters,omitempty" yaml:"parameters,omitempty"`
	RequestBody *RequestBody         `json:"requestBody,omitempty" yaml:"requestBody,omitempty"`
	Responses   map[string]*Response `json:"responses" yaml:"responses"`
	Deprecated  bool                 `json:"deprecated,omitempty" yaml:"deprecated,omitempty"`
	Extensions  Extensions           `json:"-" yaml:",inline"`
}

func (o Operation) MarshalJSON() ([]byte, error) {
	type plain Operation
	b, err := json.Marshal(plain(o))
	if err != nil {
		return nil, err
	}
	return o.Extensions.appendJSON(b)
}

type Parameter struct {
	Name        string  `json:"name" yaml:"name"`
	In          string  `json:"in" yaml:"in"`
	Description string  `json:"description,omitempty" yaml:"description,omitempty"`
	Required    bool    `json:"required,omitempty" yaml:"required,omitempty"`
	Deprecated  bool    `json:"deprecated,omitempty" yaml:"deprecated,omitempty"`
	Style       string  `json:"style,omitempty" yaml:"style,omitempty"`
	Explode     *bool   `json:"explode,omitempty" yaml:"explode,omitempty"`
	Schema      *Schema `json:"schema,omitempty" yaml:"schema,omitempty"`
}

type RequestBody struct {
	Description string                `json:"description,omitempty" yaml:"description,omitempty"`
	Content     map[string]*MediaType `json:"content" yaml:"content"`
	Required    bool                  `json:"required,omitempty" yaml:"required,omitempty"`
}

type MediaType struct {
	Schema *Schema `json:"schema,omitempty" yaml:"schema,omitempty"`
}

type Response struct {
	Description string                `json:"description" yaml:"description"`
	Content     map[string]*MediaType `json:"content,omitempty" yaml:"content,omitempty"`
}

type Components struct {
	Schemas map[string]*Schema `json:"schemas,omitempty" yaml:"schemas,omitempty"`
}

type Tag struct {
	Name         string        `json:"name" yaml:"name"`
	Description  string        `json:"description,omitempty" yaml:"description,omitempty"`
	ExternalDocs *ExternalDocs `json:"externalDocs,omitempty" yaml:"externalDocs,omitempty"`
}

type ExternalDocs struct {
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	URL         string `json:"url" yaml:"url"`
}

type Schema struct {
	Ref                  string     `json:"$ref,omitempty" yaml:"$ref,omitempty"`
	Title                string     `json:"title,omitempty" yaml:"title,omitempty"`
	Description          string     `json:"description,omitempty" yaml:"description,omitempty"`
	Type                 Types      `json:"type,omitempty" yaml:"type,omitempty"`
	Format               string     `json:"format,omitempty" yaml:"format,omitempty"`
	Pattern              string     `json:"pattern,omitempty" yaml:"pattern,omitempty"`
	Enum                 []string   `json:"enum,omitempty" yaml:"enum,omitempty"`
	Properties           Properties `json:"properties,omitempty" yaml:"properties,omitempty"`
	Required             []string   `json:"required,omitempty" yaml:"required,omitempty"`
	AdditionalProperties *Schema    `json:"additionalProperties,omitempty" yaml:"additionalProperties,omitempty"`
	Items                *Schema    `json:"items,omitempty" yaml:"items,omitempty"`
	AnyOf                []*Schema  `json:"anyOf,omitempty" yaml:"anyOf,omitempty"`
	Minimum              *float64   `json:"minimum,omitempty" yaml:"minimum,omitempty"`
	Maximum              *float64   `json:"maximum,omitempty" yaml:"maximum,omitempty"`
	MinLength            *uint64    `json:"minLength,omitempty" yaml:"minLength,omitempty"`
	MaxLength            *uint64    `json:"maxLength,omitempty" yaml:"maxLength,omitempty"`
	MinItems             *uint64    `json:"minItems,omitempty" yaml:"minItems,omitempty"`
	MaxItems             *uint64    `json:"maxItems,omitempty" yaml:"maxItems,omitempty"`
	ReadOnly             bool       `json:"readOnly,omitempty" yaml:"readOnly,omitempty"`
	WriteOnly            bool       `json:"writeOnly,omitempty" yaml:"writeOnly,omitempty"`
	Deprecated           bool       `json:"deprecated,omitempty" yaml:"deprecated,omitempty"`
	Examples             []any      `json:"examples,omitempty" yaml:"examples,omitempty"`
	Extensions           Extensions `json:"-" yaml:",inline"`
}

func (s Schema) MarshalJSON() ([]byte, error) {
	type plain Schema
	b, err := json.Marshal(plain(s))
	if err != nil {
		return nil, err
	}
	return s.Extensions.appendJSON(b)
}

// Extensions are `x-*` specification extensions, serialized inline in the
// object holding them.
type Extensions map[string]any

// appendJSON adds the extensions to a marshaled JSON object, sorted by key.
func (e Extensions) appendJSON(obj []byte) ([]byte, error) {
	if len(e) == 0 {
		return obj, nil
	}
	keys := slices.Sorted(maps.Keys(e))

	buf := bytes.NewBuffer(obj[:len(obj)-1]) // drop the closing brace
	for i, key := range keys {
		if i > 0 || len(obj) > 2 {
			buf.WriteByte(',')
		}
		k, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}
		v, err := json.Marshal(e[key])
		if err != nil {
			return nil, err
		}
		buf.Write(k)
		buf.WriteByte(':')
		buf.Write(v)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// Types is a JSON Schema type list, serialized as a single string when it
// holds one element.
type Types []string

func (t Types) value() any {
	if len(t) == 1 {
		return t[0]
	}
	return []string(t)
}

func (t Types) MarshalJSON() ([]byte, error) { return json.Marshal(t.value()) }

func (t Types) MarshalYAML() (any, error) { return t.value(), nil }

// Property is a named schema in Properties.
type Property struct {
	Name   string
	Schema *Schema
}

// Properties is an ordered set of schema properties, kept in proto field order.
type Properties []Property

func (p Properties) MarshalJSON() ([]byte, error) {
	buf := []byte{'{'}
	for i, prop := range p {
		if i > 0 {
			buf = append(buf, ',')
		}
		key, err := json.Marshal(prop.Name)
		if err != nil {
			return nil, err
		}
		val, err := json.Marshal(prop.Schema)
		if err != nil {
			return nil, err
		}
		buf = append(buf, key...)
		buf = append(buf, ':')
		buf = append(buf, val...)
	}
	return append(buf, '}'), nil
}

func (p Properties) MarshalYAML() (any, error) {
	node := &yaml.Node{Kind: yaml.MappingNode}
	for _, prop := range p {
		var val yaml.Node
		if err := val.Encode(prop.Schema); err != nil {
			return nil, err
		}
		node.Content = append(node.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: prop.Name},
			&val,
		)
	}
	return node, nil
}
