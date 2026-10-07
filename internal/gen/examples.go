package gen

import (
	"fmt"
	"mime"
	"os"
	"path"
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/pckt-sh/openapi/internal/openapi"
	pckt "github.com/pckt-sh/openapi/proto/pckt/openapi"
)

const (
	jsonContentType   = "application/json"
	binaryContentType = "application/octet-stream"
	httpBodyName      = "google.api.HttpBody"
)

// isHTTPBody reports whether a message is google.api.HttpBody, which
// grpc-gateway sends as a raw body with its own content type.
func isHTTPBody(m protoreflect.MessageDescriptor) bool {
	return m != nil && m.FullName() == httpBodyName
}

func rawSchema() *openapi.Schema {
	return &openapi.Schema{Type: openapi.Types{"string"}, Format: "binary"}
}

// mediaType returns the content type of a body: the annotation value, or the
// default for JSON messages and raw google.api.HttpBody bodies.
func mediaType(annotated string, raw bool) string {
	switch {
	case annotated != "":
		return annotated
	case raw:
		return binaryContentType
	default:
		return jsonContentType
	}
}

func isJSONMediaType(contentType string) bool {
	mt, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mt = contentType
	}
	return mt == jsonContentType || strings.HasSuffix(mt, "+json")
}

// exampleFiles reads example files from the examples_dir option. The
// directory is opened on first use, so it only has to exist when files are
// referenced, and through os.Root so that paths cannot escape it.
type exampleFiles struct {
	dir  string
	root *os.Root
}

func (e *exampleFiles) read(name string) ([]byte, error) {
	if e.root == nil {
		root, err := os.OpenRoot(e.dir)
		if err != nil {
			return nil, fmt.Errorf("open examples_dir: %w", err)
		}
		e.root = root
	}
	return e.root.ReadFile(name)
}

func (e *exampleFiles) Close() error {
	if e.root == nil {
		return nil
	}
	return e.root.Close()
}

// example is the content of an Example annotation, decoded per binding once
// its content type is known.
type example struct {
	data []byte
	yaml bool
}

type methodExamples struct {
	request, response *example
}

func (g *fileGen) loadExamples(oo *pckt.OperationOptions) (methodExamples, error) {
	var (
		out methodExamples
		err error
	)
	if out.request, err = g.loadExample(oo.GetRequestExample()); err != nil {
		return out, fmt.Errorf("request_example: %w", err)
	}
	if out.response, err = g.loadExample(oo.GetResponseExample()); err != nil {
		return out, fmt.Errorf("response_example: %w", err)
	}
	return out, nil
}

func (g *fileGen) loadExample(ex *pckt.Example) (*example, error) {
	switch {
	case ex.HasValue():
		return &example{data: []byte(ex.GetValue())}, nil
	case ex.HasFile():
		if g.exampleFiles == nil {
			return nil, fmt.Errorf("example files are not available")
		}
		data, err := g.exampleFiles.read(ex.GetFile())
		if err != nil {
			return nil, err
		}
		ext := path.Ext(ex.GetFile())
		return &example{data: data, yaml: ext == ".yaml" || ext == ".yml"}, nil
	}
	return nil, nil
}

// decode returns the example value for a content type: parsed JSON (or YAML)
// for JSON content types, the raw text otherwise.
func (e *example) decode(contentType string) (any, error) {
	if e == nil {
		return nil, nil
	}
	if !isJSONMediaType(contentType) {
		return string(e.data), nil
	}

	var (
		v   openapi.Node
		err error
	)
	if e.yaml {
		v, err = openapi.ParseYAML(e.data)
	} else {
		v, err = openapi.ParseJSON(string(e.data))
	}
	if err != nil {
		return nil, fmt.Errorf("invalid example: %w", err)
	}
	return v, nil
}
