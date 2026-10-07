package openapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/go-faster/yaml"
)

// Node is a JSON value kept as a YAML node, so that object keys keep the
// order they were written in, in both YAML and JSON output.
type Node struct {
	*yaml.Node
}

func (n Node) MarshalYAML() (any, error) { return n.Node, nil }

func (n Node) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	if err := WriteJSON(&buf, n.Node); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ParseJSON parses a single JSON value into an ordered Node.
func ParseJSON(data string) (Node, error) {
	dec := json.NewDecoder(strings.NewReader(data))
	dec.UseNumber()
	n, err := parseJSONValue(dec)
	if err != nil {
		return Node{}, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return Node{}, fmt.Errorf("unexpected data after the JSON value")
	}
	return Node{n}, nil
}

func parseJSONValue(dec *json.Decoder) (*yaml.Node, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}

	switch v := tok.(type) {
	case json.Delim:
		switch v {
		case '{':
			n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			for dec.More() {
				key, err := dec.Token()
				if err != nil {
					return nil, err
				}
				val, err := parseJSONValue(dec)
				if err != nil {
					return nil, err
				}
				n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key.(string)}, val)
			}
			_, err := dec.Token() // }
			return n, err
		case '[':
			n := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
			for dec.More() {
				val, err := parseJSONValue(dec)
				if err != nil {
					return nil, err
				}
				n.Content = append(n.Content, val)
			}
			_, err := dec.Token() // ]
			return n, err
		}
		return nil, fmt.Errorf("unexpected delimiter %v", v)
	case string:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}, nil
	case json.Number:
		tag := "!!int"
		if strings.ContainsAny(v.String(), ".eE") {
			tag = "!!float"
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: v.String()}, nil
	case bool:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: fmt.Sprint(v)}, nil
	case nil:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}, nil
	}
	return nil, fmt.Errorf("unexpected JSON token %v", tok)
}

// ParseYAML parses a YAML document into an ordered Node.
func ParseYAML(data []byte) (Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return Node{}, err
	}
	if len(doc.Content) == 0 {
		return Node{}, fmt.Errorf("empty YAML document")
	}
	return Node{doc.Content[0]}, nil
}

// WriteJSON writes a YAML node as compact JSON, keeping key order.
func WriteJSON(buf *bytes.Buffer, n *yaml.Node) error {
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			buf.WriteString("null")
			return nil
		}
		return WriteJSON(buf, n.Content[0])
	case yaml.AliasNode:
		return WriteJSON(buf, n.Alias)
	case yaml.MappingNode:
		buf.WriteByte('{')
		for i := 0; i+1 < len(n.Content); i += 2 {
			if i > 0 {
				buf.WriteByte(',')
			}
			key, err := json.Marshal(n.Content[i].Value)
			if err != nil {
				return err
			}
			buf.Write(key)
			buf.WriteByte(':')
			if err := WriteJSON(buf, n.Content[i+1]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	case yaml.SequenceNode:
		buf.WriteByte('[')
		for i, c := range n.Content {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := WriteJSON(buf, c); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	case yaml.ScalarNode:
		var v any
		if n.ShortTag() == "!!str" {
			v = n.Value
		} else if err := n.Decode(&v); err != nil {
			return fmt.Errorf("line %d: %w", n.Line, err)
		}
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("line %d: %w", n.Line, err)
		}
		buf.Write(b)
	default:
		return fmt.Errorf("unexpected YAML node kind %v", n.Kind)
	}
	return nil
}
