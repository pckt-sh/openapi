package merge

import (
	"bytes"
	"encoding/json"

	"github.com/go-faster/yaml"

	"github.com/pckt-sh/openapi/internal/openapi"
)

// EncodeJSON writes the document as indented JSON, keeping key order.
func EncodeJSON(root *yaml.Node) ([]byte, error) {
	var compact bytes.Buffer
	if err := openapi.WriteJSON(&compact, root); err != nil {
		return nil, err
	}

	var out bytes.Buffer
	if err := json.Indent(&out, compact.Bytes(), "", "  "); err != nil {
		return nil, err
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}
