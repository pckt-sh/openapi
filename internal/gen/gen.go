// Package gen implements protoc-gen-pckt-openapi: it turns every .proto file of a
// CodeGeneratorRequest into its own OpenAPI 3.1 document.
package gen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path"
	"strings"

	"github.com/go-faster/yaml"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"

	"github.com/pckt-sh/openapi/internal/merge"
	"github.com/pckt-sh/openapi/internal/openapi"
)

// Options are the plugin parameters, passed as `key=value` pairs separated by commas.
type Options struct {
	// Format is the output format: yaml (default) or json.
	Format string
	// Title is the info.title of each document, defaults to the proto file path
	// (or "API" for the merged document).
	Title string
	// Version is the info.version of each document.
	Version string
	// Merge, when set, is the path of a single document merging the documents
	// of every file to generate, written instead of one document per file.
	// Its format follows the extension: .json for JSON, YAML otherwise.
	Merge string
	// Servers are the server URLs of the merged document, `server` can be repeated.
	Servers []string
}

// ParseOptions parses the plugin parameter string.
func ParseOptions(param string) (Options, error) {
	opts := Options{Format: "yaml", Version: "0.0.0"}
	for kv := range strings.SplitSeq(param, ",") {
		if kv = strings.TrimSpace(kv); kv == "" {
			continue
		}
		key, value, _ := strings.Cut(kv, "=")
		switch key {
		case "format":
			if value != "yaml" && value != "json" {
				return opts, fmt.Errorf("invalid format %q: must be yaml or json", value)
			}
			opts.Format = value
		case "title":
			opts.Title = value
		case "version":
			opts.Version = value
		case "merge":
			if value == "" {
				return opts, fmt.Errorf("merge requires an output file path")
			}
			opts.Merge = value
		case "server":
			opts.Servers = append(opts.Servers, value)
		default:
			return opts, fmt.Errorf("unknown parameter %q", key)
		}
	}
	return opts, nil
}

// Run generates one OpenAPI document per file to generate, or a single merged
// document when the merge option is set.
func Run(req *pluginpb.CodeGeneratorRequest) (*pluginpb.CodeGeneratorResponse, error) {
	opts, err := ParseOptions(req.GetParameter())
	if err != nil {
		return nil, err
	}

	files, err := protodesc.NewFiles(&descriptorpb.FileDescriptorSet{File: req.GetProtoFile()})
	if err != nil {
		return nil, fmt.Errorf("build file registry: %w", err)
	}

	resp := &pluginpb.CodeGeneratorResponse{
		SupportedFeatures: proto.Uint64(uint64(
			pluginpb.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL |
				pluginpb.CodeGeneratorResponse_FEATURE_SUPPORTS_EDITIONS,
		)),
		MinimumEdition: proto.Int32(int32(descriptorpb.Edition_EDITION_PROTO2)),
		MaximumEdition: proto.Int32(int32(descriptorpb.Edition_EDITION_2023)),
	}

	format := opts.Format
	if opts.Merge != "" {
		// Per-file documents are only merge inputs, YAML keeps them readable in errors.
		format = "yaml"
	}

	var inputs []merge.Input
	for _, name := range req.GetFileToGenerate() {
		fd, err := files.FindFileByPath(name)
		if err != nil {
			return nil, err
		}

		doc, err := newFileGen(opts, fd).generate()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		if doc == nil {
			continue
		}

		content, err := Encode(doc, format)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}

		if opts.Merge != "" {
			inputs = append(inputs, merge.Input{Name: name, Data: content})
			continue
		}
		resp.File = append(resp.File, &pluginpb.CodeGeneratorResponse_File{
			Name:    proto.String(strings.TrimSuffix(name, ".proto") + ".openapi." + format),
			Content: proto.String(string(content)),
		})
	}

	if opts.Merge != "" && len(inputs) > 0 {
		content, err := mergeDocuments(inputs, opts)
		if err != nil {
			return nil, err
		}
		resp.File = append(resp.File, &pluginpb.CodeGeneratorResponse_File{
			Name:    proto.String(opts.Merge),
			Content: proto.String(string(content)),
		})
	}

	return resp, nil
}

// mergeDocuments merges per-file documents with the same rules as pckt-openapi-merge.
func mergeDocuments(inputs []merge.Input, opts Options) ([]byte, error) {
	doc, err := merge.Merge(inputs, merge.Options{
		Title:   opts.Title,
		Version: opts.Version,
		Servers: opts.Servers,
	})
	if err != nil {
		return nil, fmt.Errorf("merge: %w", err)
	}
	if strings.EqualFold(path.Ext(opts.Merge), ".json") {
		return merge.EncodeJSON(doc)
	}
	return merge.EncodeYAML(doc)
}

// Encode serializes a document as yaml or json.
func Encode(doc *openapi.Document, format string) ([]byte, error) {
	var buf bytes.Buffer
	switch format {
	case "json":
		enc := json.NewEncoder(&buf)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		if err := enc.Encode(doc); err != nil {
			return nil, err
		}
	default:
		enc := yaml.NewEncoder(&buf)
		enc.SetIndent(2)
		if err := enc.Encode(doc); err != nil {
			return nil, err
		}
		if err := enc.Close(); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}
