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
	// infer replaces the body schema with one inferred from the example.
	infer bool
}

type methodExamples struct {
	request, response *example
	// requestName and responseName are the component names of inferred schemas.
	requestName, responseName string
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
		return &example{data: []byte(ex.GetValue()), infer: ex.GetInferSchema()}, nil
	case ex.HasFile():
		if g.exampleFiles == nil {
			return nil, fmt.Errorf("example files are not available")
		}
		data, err := g.exampleFiles.read(ex.GetFile())
		if err != nil {
			return nil, err
		}
		ext := path.Ext(ex.GetFile())
		return &example{data: data, yaml: ext == ".yaml" || ext == ".yml", infer: ex.GetInferSchema()}, nil
	case ex.GetInferSchema():
		return nil, fmt.Errorf("infer_schema requires a value or a file")
	}
	return nil, nil
}

// inferredSchema infers the schema of a decoded example, registers it as a
// component and returns a reference to it.
func (g *fileGen) inferredSchema(name string, value any, contentType string) (*openapi.Schema, error) {
	node, ok := value.(openapi.Node)
	if !ok || !isJSONMediaType(contentType) {
		return nil, fmt.Errorf("infer_schema requires a JSON content type, got %s", contentType)
	}
	if _, exists := g.schemas[name]; exists && !g.inferred[name] {
		return nil, fmt.Errorf("inferred schema name %s is already used by a message schema", name)
	}
	// The example is not written with the inferred schema: it may be large,
	// and the schema carries a sample value for every property instead.
	g.schemas[name] = inferSchema(node.Node)
	g.inferred[name] = true
	return ref(name), nil
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
