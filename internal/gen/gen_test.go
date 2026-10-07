package gen

import (
	"encoding/json"
	"flag"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/go-faster/yaml"
	"github.com/ogen-go/ogen"
	"github.com/ogen-go/ogen/openapi/parser"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/pluginpb"
)

var update = flag.Bool("update", false, "update golden files")

const (
	descriptorPath = "../../testdata/descriptor.binpb"
	goldenDir      = "../../testdata/golden"
)

func request(t *testing.T, param string) *pluginpb.CodeGeneratorRequest {
	t.Helper()

	data, err := os.ReadFile(descriptorPath)
	if err != nil {
		t.Fatal(err)
	}
	set := &descriptorpb.FileDescriptorSet{}
	if err := proto.Unmarshal(data, set); err != nil {
		t.Fatal(err)
	}

	req := &pluginpb.CodeGeneratorRequest{Parameter: proto.String(param), ProtoFile: set.GetFile()}
	for _, f := range set.GetFile() {
		if strings.HasPrefix(f.GetName(), "example/") {
			req.FileToGenerate = append(req.FileToGenerate, f.GetName())
		}
	}
	if len(req.FileToGenerate) == 0 {
		t.Fatal("no example files in descriptor set")
	}
	return req
}

// examplesDir is the examples_dir of the testdata, relative to this package.
const examplesDir = "../../testdata/examples"

func generate(t *testing.T, param string) map[string]string {
	t.Helper()
	resp, err := Run(request(t, param+",examples_dir="+examplesDir))
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetError() != "" {
		t.Fatal(resp.GetError())
	}
	files := map[string]string{}
	for _, f := range resp.GetFile() {
		files[f.GetName()] = f.GetContent()
	}
	return files
}

func checkGolden(t *testing.T, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(goldenDir, name)
		if *update {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}

		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v (run go test ./internal/gen -update)", name, err)
		}
		if string(want) != content {
			t.Errorf("%s differs from golden file, run go test ./internal/gen -update and review the diff", name)
		}
	}
}

// validate parses the document with ogen to check it is a usable OpenAPI spec.
func validate(t *testing.T, name, content string) {
	t.Helper()
	spec, err := ogen.Parse([]byte(content))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if _, err := parser.Parse(spec, parser.Settings{}); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}

func TestGoldenYAML(t *testing.T) {
	files := generate(t, "format=yaml")
	for name, content := range files {
		if !strings.HasSuffix(name, ".openapi.yaml") {
			t.Errorf("unexpected file name %s", name)
		}
		validate(t, name, content)
	}
	checkGolden(t, files)
}

func TestGoldenJSON(t *testing.T) {
	files := generate(t, "format=json")
	for name, content := range files {
		if !strings.HasSuffix(name, ".openapi.json") {
			t.Errorf("unexpected file name %s", name)
		}
		if !json.Valid([]byte(content)) {
			t.Fatalf("%s: invalid JSON", name)
		}
		validate(t, name, content)
	}
	checkGolden(t, files)
}

// TestJSONMatchesYAML ensures both formats carry the same document, as
// extensions and ordered properties are marshaled separately for each.
func TestJSONMatchesYAML(t *testing.T) {
	yamlFiles := generate(t, "format=yaml")
	jsonFiles := generate(t, "format=json,title=Example,version=1.2.3")
	if len(yamlFiles) != len(jsonFiles) {
		t.Fatalf("got %d YAML files and %d JSON files", len(yamlFiles), len(jsonFiles))
	}

	for name, y := range yamlFiles {
		j, ok := jsonFiles[strings.TrimSuffix(name, ".yaml")+".json"]
		if !ok {
			t.Fatalf("no JSON counterpart for %s", name)
		}

		var fromYAML, fromJSON map[string]any
		if err := yaml.Unmarshal([]byte(y), &fromYAML); err != nil {
			t.Fatal(err)
		}
		if err := yaml.Unmarshal([]byte(j), &fromJSON); err != nil {
			t.Fatal(err)
		}

		info := fromJSON["info"].(map[string]any)
		if info["title"] != "Example" || info["version"] != "1.2.3" {
			t.Errorf("%s: info parameters not applied: %v", name, info)
		}
		fromJSON["info"] = fromYAML["info"]

		if !reflect.DeepEqual(fromYAML, fromJSON) {
			t.Errorf("%s: JSON and YAML documents differ", name)
		}
	}
}

func TestExtensions(t *testing.T) {
	g := newFileGen(Options{}, nil)
	ext := g.extensions("pkg.Msg", map[string]*structpb.Value{
		"x-ok":    structpb.NewStringValue("v"),
		"no-dash": structpb.NewBoolValue(true),
	})
	if len(ext) != 1 || ext["x-ok"] != "v" {
		t.Errorf("unexpected extensions %v", ext)
	}
	if len(g.errs) != 1 || !strings.Contains(g.errs[0].Error(), `"no-dash" must start with "x-"`) {
		t.Errorf("unexpected errors %v", g.errs)
	}
}

func TestParseOptions(t *testing.T) {
	if _, err := ParseOptions("format=xml"); err == nil {
		t.Error("expected error for invalid format")
	}
	if _, err := ParseOptions("merge="); err == nil {
		t.Error("expected error for empty merge path")
	}
	if _, err := ParseOptions("unknown=1"); err == nil {
		t.Error("expected error for unknown parameter")
	}
	opts, err := ParseOptions(" format=json , title=A=B ,server=https://a,server=https://b")
	if err != nil {
		t.Fatal(err)
	}
	if opts.Format != "json" || opts.Title != "A=B" || len(opts.Servers) != 2 {
		t.Errorf("unexpected options %+v", opts)
	}
}

func TestSegmentsPattern(t *testing.T) {
	for in, want := range map[string]string{
		"":                   "",
		"*":                  "",
		"shelves/*":          "^shelves/[^/]+$",
		"shelves/*/books/**": "^shelves/[^/]+/books/.+$",
	} {
		if got := segmentsPattern(in); got != want {
			t.Errorf("segmentsPattern(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestMerge checks the merge option produces the same document as
// pckt-openapi-merge on the per-file documents (see internal/merge goldens).
func TestMerge(t *testing.T) {
	for _, name := range []string{"openapi.yaml", "openapi.json"} {
		t.Run(name, func(t *testing.T) {
			files := generate(t, "format=json,merge=api/"+name+",title=Example API,version=1.0.0,server=https://api.example.com|REST API")
			if len(files) != 1 {
				t.Fatalf("got %d files, want only the merged one", len(files))
			}
			got, ok := files["api/"+name]
			if !ok {
				t.Fatalf("missing merged file, got %v", slices.Collect(maps.Keys(files)))
			}
			validate(t, name, got)
			if *update {
				return // merge goldens are updated by internal/merge, after this package
			}

			want, err := os.ReadFile(filepath.Join(goldenDir, name))
			if err != nil {
				t.Fatal(err)
			}
			if got != string(want) {
				t.Errorf("merged document differs from %s", name)
			}
		})
	}
}
