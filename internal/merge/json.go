package merge

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/go-faster/yaml"
)

// EncodeJSON writes the document as indented JSON, keeping key order.
func EncodeJSON(root *yaml.Node) ([]byte, error) {
	var compact bytes.Buffer
	if err := writeJSON(&compact, root); err != nil {
		return nil, err
	}

	var out bytes.Buffer
	if err := json.Indent(&out, compact.Bytes(), "", "  "); err != nil {
		return nil, err
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}

func writeJSON(buf *bytes.Buffer, n *yaml.Node) error {
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			buf.WriteString("null")
			return nil
		}
		return writeJSON(buf, n.Content[0])
	case yaml.AliasNode:
		return writeJSON(buf, n.Alias)
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
			if err := writeJSON(buf, n.Content[i+1]); err != nil {
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
			if err := writeJSON(buf, c); err != nil {
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
