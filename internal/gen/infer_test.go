package gen

import (
	"encoding/json"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/pckt-sh/openapi/internal/openapi"
	pckt "github.com/pckt-sh/openapi/proto/pckt/openapi"
)

func inferJSON(t *testing.T, example string) string {
	t.Helper()
	n, err := openapi.ParseJSON(example)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(inferSchema(n.Node))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestInferSchema(t *testing.T) {
	tests := map[string]struct{ example, want string }{
		"scalars": {
			`{"s": "a", "i": 1, "f": 1.5, "b": true, "n": null}`,
			`{"type":"object","properties":{"s":{"type":"string","examples":["a"]},"i":{"type":"integer","examples":[1]},"f":{"type":"number","examples":[1.5]},"b":{"type":"boolean","examples":[true]},"n":{"type":"null"}}}`,
		},
		"property order is kept": {
			`{"z": 1, "a": 2, "m": 3}`,
			`{"type":"object","properties":{"z":{"type":"integer","examples":[1]},"a":{"type":"integer","examples":[2]},"m":{"type":"integer","examples":[3]}}}`,
		},
		"array items are merged": {
			`[{"id": 1, "price": 10}, {"id": 2, "price": 10.5, "note": null}, {"id": 3, "note": "gift"}]`,
			`{"type":"array","items":{"type":"object","properties":{"id":{"type":"integer","examples":[1]},"price":{"type":"number","examples":[10]},"note":{"type":["string","null"],"examples":["gift"]}}}}`,
		},
		"first non-null value is the example": {
			`[{"v": null}, {"v": "x"}, {"v": "y"}]`,
			`{"type":"array","items":{"type":"object","properties":{"v":{"type":["string","null"],"examples":["x"]}}}}`,
		},
		"long strings are not kept as example": {
			`{"blob": "` + strings.Repeat("a", maxExampleLength+1) + `", "short": "` + strings.Repeat("a", maxExampleLength) + `"}`,
			`{"type":"object","properties":{"blob":{"type":"string"},"short":{"type":"string","examples":["` + strings.Repeat("a", maxExampleLength) + `"]}}}`,
		},
		"empty array": {
			`{"tags": []}`,
			`{"type":"object","properties":{"tags":{"type":"array","items":{}}}}`,
		},
		"mixed types": {
			`[1, "a", {"k": true}]`,
			`{"type":"array","items":{"type":["object","string","integer"],"properties":{"k":{"type":"boolean","examples":[true]}},"examples":[1]}}`,
		},
		"formats": {
			`{"id": "3f0c5a8e-6c1b-4f5e-9a3d-2b1c0d9e8f7a", "at": "2026-10-09T12:00:00Z", "day": "2026-10-09", "mail": "a@b.co", "url": "https://pckt.sh/x", "plain": "hello"}`,
			`{"type":"object","properties":{"id":{"type":"string","format":"uuid","examples":["3f0c5a8e-6c1b-4f5e-9a3d-2b1c0d9e8f7a"]},"at":{"type":"string","format":"date-time","examples":["2026-10-09T12:00:00Z"]},"day":{"type":"string","format":"date","examples":["2026-10-09"]},"mail":{"type":"string","format":"email","examples":["a@b.co"]},"url":{"type":"string","format":"uri","examples":["https://pckt.sh/x"]},"plain":{"type":"string","examples":["hello"]}}}`,
		},
		"inconsistent format": {
			`[{"v": "2026-10-09"}, {"v": "tomorrow"}]`,
			`{"type":"array","items":{"type":"object","properties":{"v":{"type":"string","examples":["2026-10-09"]}}}}`,
		},
		"nested": {
			`{"data": {"items": [{"sizes": [{"eu": "42", "price": 120}]}]}}`,
			`{"type":"object","properties":{"data":{"type":"object","properties":{"items":{"type":"array","items":{"type":"object","properties":{"sizes":{"type":"array","items":{"type":"object","properties":{"eu":{"type":"string","examples":["42"]},"price":{"type":"integer","examples":[120]}}}}}}}}}}}`,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := inferJSON(t, tt.example); got != tt.want {
				t.Errorf("got  %s\nwant %s", got, tt.want)
			}
		})
	}
}

func TestInferSchemaYAML(t *testing.T) {
	n, err := openapi.ParseYAML([]byte("name: Mug\nprice: 990\ncreated: 2026-10-09\n"))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(inferSchema(n.Node))
	want := `{"type":"object","properties":{"name":{"type":"string","examples":["Mug"]},"price":{"type":"integer","examples":[990]},"created":{"type":"string","format":"date","examples":["2026-10-09"]}}}`
	if string(b) != want {
		t.Errorf("got  %s\nwant %s", b, want)
	}
}

func inferExample(v string) *pckt.Example {
	return pckt.Example_builder{Value: proto.String(v), InferSchema: true}.Build()
}

func TestInferredComponents(t *testing.T) {
	oo := pckt.OperationOptions_builder{
		OperationId:     "createThing",
		RequestExample:  inferExample(`{"name": "a"}`),
		ResponseExample: inferExample(`{"id": 1}`),
	}.Build()
	resp, err := Run(methodRequest(post("/v1/m", "*"), oo, ""))
	if err != nil {
		t.Fatal(err)
	}
	got := resp.GetFile()[0].GetContent()
	for _, want := range []string{
		"$ref: '#/components/schemas/createThingRequest'",
		"$ref: '#/components/schemas/createThingResponse'",
		"createThingRequest:\n      type: object\n      properties:\n        name:\n          type: string",
		"createThingResponse:\n      type: object\n      properties:\n        id:\n          type: integer\n          examples:\n            - 1",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output misses %q:\n%s", want, got)
		}
	}
	// The full examples are replaced by per-property examples.
	if strings.Contains(got, "example:") {
		t.Errorf("inferred bodies must not carry the full example:\n%s", got)
	}
}

func TestInferErrors(t *testing.T) {
	tests := map[string]struct {
		oo   *pckt.OperationOptions
		want string
	}{
		"non-JSON content type": {
			oo: pckt.OperationOptions_builder{
				ResponseExample:     inferExample(`a,b`),
				ResponseContentType: "text/csv",
			}.Build(),
			want: "infer_schema requires a JSON content type, got text/csv",
		},
		"no example": {
			oo:   pckt.OperationOptions_builder{ResponseExample: pckt.Example_builder{InferSchema: true}.Build()}.Build(),
			want: "infer_schema requires a value or a file",
		},
		"name used by a message": {
			oo:   pckt.OperationOptions_builder{OperationId: "t.M", ResponseExample: inferExample(`{}`)}.Build(),
			want: "t.MResponse: schema name is already used by a schema inferred from an example",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := Run(methodRequest(get("/v1/m"), tt.oo, ""))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got error %v, want %q", err, tt.want)
			}
		})
	}
}
