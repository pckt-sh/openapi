package gen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"

	pckt "github.com/pckt-sh/openapi/proto/pckt/openapi"
)

// methodRequest builds a request for a single `t.S.M(t.Req) returns (t.Resp)`
// method with the given HTTP rule and operation options.
func methodRequest(rule *annotations.HttpRule, oo *pckt.OperationOptions, param string) *pluginpb.CodeGeneratorRequest {
	mo := &descriptorpb.MethodOptions{}
	proto.SetExtension(mo, annotations.E_Http, rule)
	proto.SetExtension(mo, pckt.E_Operation, oo)

	file := &descriptorpb.FileDescriptorProto{
		Name:       proto.String("t/t.proto"),
		Package:    proto.String("t"),
		Syntax:     proto.String("proto3"),
		Dependency: []string{"google/api/annotations.proto", "pckt/openapi/annotations.proto"},
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: proto.String("Req"), Field: []*descriptorpb.FieldDescriptorProto{{
				Name:     proto.String("id"),
				JsonName: proto.String("id"),
				Number:   proto.Int32(1),
				Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
				Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			}}},
			{Name: proto.String("Resp")},
			// MResponse collides with the inferred response schema of operation `t.M`.
			{Name: proto.String("MResponse")},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name: proto.String("S"),
			Method: []*descriptorpb.MethodDescriptorProto{{
				Name:       proto.String("M"),
				InputType:  proto.String(".t.Req"),
				OutputType: proto.String(".t.Resp"),
				Options:    mo,
			}},
		}},
	}

	var files []*descriptorpb.FileDescriptorProto
	seen := map[string]bool{}
	var add func(fd protoreflect.FileDescriptor)
	add = func(fd protoreflect.FileDescriptor) {
		if seen[fd.Path()] {
			return
		}
		seen[fd.Path()] = true
		imports := fd.Imports()
		for i := range imports.Len() {
			add(imports.Get(i).FileDescriptor)
		}
		files = append(files, protodesc.ToFileDescriptorProto(fd))
	}
	add(annotations.File_google_api_annotations_proto)
	add(pckt.File_pckt_openapi_annotations_proto)

	return &pluginpb.CodeGeneratorRequest{
		Parameter:      proto.String(param),
		FileToGenerate: []string{"t/t.proto"},
		ProtoFile:      append(files, file),
	}
}

func get(path string) *annotations.HttpRule {
	return &annotations.HttpRule{Pattern: &annotations.HttpRule_Get{Get: path}}
}

func post(path, body string) *annotations.HttpRule {
	return &annotations.HttpRule{Pattern: &annotations.HttpRule_Post{Post: path}, Body: body}
}

func fileExample(name string) *pckt.Example {
	return pckt.Example_builder{File: proto.String(name)}.Build()
}

func valueExample(v string) *pckt.Example {
	return pckt.Example_builder{Value: proto.String(v)}.Build()
}

func TestExampleErrors(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "invalid.json"), []byte(`{"a": `), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(dir), "outside.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := map[string]struct {
		rule *annotations.HttpRule
		oo   *pckt.OperationOptions
		want string
	}{
		"missing file": {
			rule: get("/v1/m"),
			oo:   pckt.OperationOptions_builder{ResponseExample: fileExample("missing.json")}.Build(),
			want: "response_example",
		},
		"file outside examples_dir": {
			rule: get("/v1/m"),
			oo:   pckt.OperationOptions_builder{ResponseExample: fileExample("../outside.json")}.Build(),
			want: "path escapes",
		},
		"invalid JSON file": {
			rule: get("/v1/m"),
			oo:   pckt.OperationOptions_builder{ResponseExample: fileExample("invalid.json")}.Build(),
			want: "invalid example",
		},
		"invalid inline JSON": {
			rule: post("/v1/m", "*"),
			oo:   pckt.OperationOptions_builder{RequestExample: valueExample(`{"a": 1} trailing`)}.Build(),
			want: "request_example: invalid example",
		},
		"request example without body": {
			rule: get("/v1/m"),
			oo:   pckt.OperationOptions_builder{RequestExample: valueExample(`{}`)}.Build(),
			want: "no HTTP binding has a body",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := Run(methodRequest(tt.rule, tt.oo, "examples_dir="+dir))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got error %v, want %q", err, tt.want)
			}
		})
	}
}

func TestExampleContentTypes(t *testing.T) {
	oo := pckt.OperationOptions_builder{
		RequestExample:      valueExample(`{"z": 1, "a": 2}`),
		RequestContentType:  "application/merge-patch+json",
		ResponseExample:     valueExample(`{"not": "parsed"}`),
		ResponseContentType: "text/plain",
	}.Build()

	resp, err := Run(methodRequest(post("/v1/m", "*"), oo, "format=json"))
	if err != nil {
		t.Fatal(err)
	}
	got := resp.GetFile()[0].GetContent()

	// +json content types are parsed, keeping key order; others are kept as text.
	for _, want := range []string{
		`"application/merge-patch+json": {`,
		`"example": {
                "z": 1,
                "a": 2
              }`,
		`"text/plain": {`,
		`"example": "{\"not\": \"parsed\"}"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output misses %s:\n%s", want, got)
		}
	}
}
