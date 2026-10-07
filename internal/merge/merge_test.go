package merge

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ogen-go/ogen"
	"github.com/ogen-go/ogen/openapi/parser"
)

var update = flag.Bool("update", false, "update golden files")

const goldenDir = "../../testdata/golden"

func mustMerge(t *testing.T, inputs []Input, opts Options) string {
	t.Helper()
	doc, err := Merge(inputs, opts)
	if err != nil {
		t.Fatal(err)
	}
	out, err := EncodeYAML(doc)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func goldenInputs(t *testing.T) []Input {
	t.Helper()
	return goldenInputsExt(t, "yaml")
}

func goldenInputsExt(t *testing.T, ext string) []Input {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(goldenDir, "example", "*.openapi."+ext))
	if err != nil || len(files) == 0 {
		t.Fatalf("no golden inputs: %v", err)
	}
	var inputs []Input
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		inputs = append(inputs, Input{Name: f, Data: data})
	}
	return inputs
}

// validate parses the document with ogen to check it is a usable OpenAPI spec.
func validate(t *testing.T, data []byte) {
	t.Helper()
	spec, err := ogen.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parser.Parse(spec, parser.Settings{}); err != nil {
		t.Fatal(err)
	}
}

func TestGolden(t *testing.T) {
	inputs := goldenInputs(t)
	got := mustMerge(t, inputs, Options{Title: "Example API", Version: "1.0.0", Servers: []Server{{URL: "https://api.example.com", Description: "REST API"}}})
	validate(t, []byte(got))

	path := filepath.Join(goldenDir, "openapi.yaml")
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != got {
		t.Error("merged document differs from golden file, run go test ./internal/merge -update and review the diff")
	}

	// Input order only changes the order of tags, paths and components are sorted.
	reversed := make([]Input, len(inputs))
	for i, in := range inputs {
		reversed[len(inputs)-1-i] = in
	}
	again := mustMerge(t, reversed, Options{Title: "Example API", Version: "1.0.0", Servers: []Server{{URL: "https://api.example.com", Description: "REST API"}}})
	withoutTags := func(s string) string { return s[:strings.Index(s, "\ntags:")] }
	if withoutTags(again) != withoutTags(got) {
		t.Error("merged paths or components depend on input order")
	}
}

// TestGoldenJSON merges the JSON documents of the generator into JSON, and
// checks it holds the same document as the YAML merge.
func TestGoldenJSON(t *testing.T) {
	opts := Options{Title: "Example API", Version: "1.0.0", Servers: []Server{{URL: "https://api.example.com", Description: "REST API"}}}
	doc, err := Merge(goldenInputsExt(t, "json"), opts)
	if err != nil {
		t.Fatal(err)
	}
	got, err := EncodeJSON(doc)
	if err != nil {
		t.Fatal(err)
	}
	validate(t, got)

	path := filepath.Join(goldenDir, "openapi.json")
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	} else {
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(want, got) {
			t.Error("merged JSON document differs from golden file, run go test ./internal/merge -update and review the diff")
		}
	}

	fromYAML := mustMerge(t, goldenInputs(t), opts)
	if fromJSON := mustMerge(t, []Input{{Name: "openapi.json", Data: got}}, opts); fromJSON != fromYAML {
		t.Error("merging JSON and YAML inputs gives different documents")
	}
}

func TestJSONRoundTrip(t *testing.T) {
	doc, err := Merge(goldenInputs(t), Options{})
	if err != nil {
		t.Fatal(err)
	}
	asJSON, err := EncodeJSON(doc)
	if err != nil {
		t.Fatal(err)
	}
	validate(t, asJSON)

	// Merging the JSON output again yields the same YAML document.
	fromJSON := mustMerge(t, []Input{{Name: "merged.json", Data: asJSON}}, Options{})
	fromYAML, err := EncodeYAML(doc)
	if err != nil {
		t.Fatal(err)
	}
	if fromJSON != string(fromYAML) {
		t.Errorf("JSON round trip changed the document:\n%s\n---\n%s", fromJSON, fromYAML)
	}
}

const docA = `
openapi: 3.1.0
info: {title: a.proto, version: 0.0.0}
paths:
  /a:
    get:
      operationId: A_Get
      responses: {"200": {description: ok}}
components:
  schemas:
    shared.Thing: {type: object, properties: {id: {type: string}}}
tags:
  - name: A
`

const docB = `
openapi: 3.1.0
info: {title: b.proto, version: 0.0.0}
paths:
  /a:
    post:
      operationId: A_Post
      responses: {"200": {description: ok}}
components:
  schemas:
    shared.Thing: {type: object, properties: {id: {type: string}}}
tags:
  - name: A
    description: The A service
`

func TestMergeDedupe(t *testing.T) {
	got := mustMerge(t, []Input{{"a", []byte(docA)}, {"b", []byte(docB)}}, Options{})
	validate(t, []byte(got))

	for _, want := range []string{"get:", "post:", "shared.Thing:", "description: The A service", "title: API"} {
		if !strings.Contains(got, want) {
			t.Errorf("merged document misses %q:\n%s", want, got)
		}
	}
	if n := strings.Count(got, "- name: A"); n != 1 {
		t.Errorf("tag A present %d times", n)
	}
	if strings.Contains(got, "a.proto") {
		t.Error("per-file info must not be kept")
	}
}

func TestMergeConflicts(t *testing.T) {
	tests := map[string]struct {
		other string
		want  string
	}{
		"path operation": {
			other: strings.Replace(docA, "A_Get", "Other_Get", 1),
			want:  "conflict on paths./a.get: defined differently in a and b",
		},
		"schema": {
			other: strings.Replace(docB, "id: {type: string}", "id: {type: integer}", 1),
			want:  "conflict on components.schemas.shared.Thing",
		},
		"operation id": {
			other: strings.Replace(strings.Replace(docB, "/a:", "/b:", 1), "A_Post", "A_Get", 1),
			want:  `duplicate operationId "A_Get"`,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := Merge([]Input{{"a", []byte(docA)}, {"b", []byte(tt.other)}}, Options{})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got error %v, want %q", err, tt.want)
			}
		})
	}
}

func TestMergeBase(t *testing.T) {
	base := `
openapi: 3.1.0
info:
  title: Base API
  version: 2.0.0
  contact: {email: api@example.com}
servers:
  - url: https://base.example.com
security:
  - bearer: []
components:
  securitySchemes:
    bearer: {type: http, scheme: bearer}
x-custom: 1
`
	var warnings bytes.Buffer
	got := mustMerge(t, []Input{{"a", []byte(docA)}}, Options{
		Base:     &Input{Name: "base", Data: []byte(base)},
		Version:  "2.1.0",
		Servers:  []Server{{URL: "https://base.example.com"}, {URL: "https://other.example.com", Description: "Other"}},
		Warnings: &warnings,
	})
	validate(t, []byte(got))

	for _, want := range []string{"title: Base API", "version: 2.1.0", "email: api@example.com", "bearer:", "x-custom: 1", "url: https://other.example.com\n    description: Other"} {
		if !strings.Contains(got, want) {
			t.Errorf("merged document misses %q:\n%s", want, got)
		}
	}
	if n := strings.Count(got, "https://base.example.com"); n != 1 {
		t.Errorf("base server present %d times", n)
	}
	if !strings.HasPrefix(got, "openapi: 3.1.0\ninfo:") {
		t.Errorf("unexpected top-level order:\n%s", got)
	}
}

func TestParseServer(t *testing.T) {
	for in, want := range map[string]Server{
		"https://api.pckt.sh":                {URL: "https://api.pckt.sh"},
		"https://grpc.pckt.sh|gRPC endpoint": {URL: "https://grpc.pckt.sh", Description: "gRPC endpoint"},
		" https://a.sh | A ":                 {URL: "https://a.sh", Description: "A"},
	} {
		got, err := ParseServer(in)
		if err != nil || got != want {
			t.Errorf("ParseServer(%q) = %+v, %v, want %+v", in, got, err, want)
		}
	}
	if _, err := ParseServer("|desc"); err == nil {
		t.Error("expected error for missing URL")
	}
}
