package gen

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/pckt-sh/openapi/internal/openapi"
	pckt "github.com/pckt-sh/openapi/proto/pckt/openapi"
)

const jsonContentType = "application/json"

// maxQueryDepth bounds the flattening of nested messages into query parameters.
const maxQueryDepth = 5

// pathVarRe matches a path template variable: `{name}` or `{name=shelves/*}`.
var pathVarRe = regexp.MustCompile(`\{([^}=]+)(?:=([^}]*))?\}`)

type fileGen struct {
	opts    Options
	file    protoreflect.FileDescriptor
	doc     *openapi.Document
	schemas map[string]*openapi.Schema
	// errs collects annotation errors found while building schemas.
	errs []error
}

func newFileGen(opts Options, file protoreflect.FileDescriptor) *fileGen {
	return &fileGen{
		opts:    opts,
		file:    file,
		schemas: map[string]*openapi.Schema{},
	}
}

func (g *fileGen) generate() (*openapi.Document, error) {
	title := g.opts.Title
	if title == "" {
		title = g.file.Path()
	}
	g.doc = &openapi.Document{
		OpenAPI: openapi.Version,
		Info:    openapi.Info{Title: title, Version: g.opts.Version},
		Paths:   openapi.Paths{},
	}

	services := g.file.Services()
	for i := range services.Len() {
		if err := g.addService(services.Get(i)); err != nil {
			return nil, err
		}
	}

	// Every message and enum of the file is exposed, even when no operation
	// references it, so files holding shared types produce reusable components.
	g.addMessages(g.file.Messages())
	enums := g.file.Enums()
	for i := range enums.Len() {
		g.enumRef(enums.Get(i))
	}

	if err := errors.Join(g.errs...); err != nil {
		return nil, err
	}
	if len(g.doc.Paths) == 0 && len(g.schemas) == 0 {
		return nil, nil
	}
	if len(g.schemas) > 0 {
		g.doc.Components = &openapi.Components{Schemas: g.schemas}
	}
	return g.doc, nil
}

func (g *fileGen) addMessages(msgs protoreflect.MessageDescriptors) {
	for i := range msgs.Len() {
		m := msgs.Get(i)
		if m.IsMapEntry() || schemaOptions(m).GetHidden() {
			continue
		}
		g.messageRef(m)
		g.addMessages(m.Messages())
		enums := m.Enums()
		for j := range enums.Len() {
			g.enumRef(enums.Get(j))
		}
	}
}

func (g *fileGen) addService(svc protoreflect.ServiceDescriptor) error {
	tag := openapi.Tag{Name: string(svc.Name()), Description: comments(svc)}
	if svc.Options() != nil && proto.HasExtension(svc.Options(), pckt.E_Tag) {
		to := proto.GetExtension(svc.Options(), pckt.E_Tag).(*pckt.TagOptions)
		if to.GetName() != "" {
			tag.Name = to.GetName()
		}
		if to.GetExternalDocs().GetUrl() != "" {
			tag.ExternalDocs = &openapi.ExternalDocs{
				Description: to.GetExternalDocs().GetDescription(),
				URL:         to.GetExternalDocs().GetUrl(),
			}
		}
	}

	added := false
	methods := svc.Methods()
	for i := range methods.Len() {
		ok, err := g.addMethod(svc, methods.Get(i), tag.Name)
		if err != nil {
			return err
		}
		added = added || ok
	}

	if added {
		g.doc.Tags = append(g.doc.Tags, tag)
	}
	return nil
}

func (g *fileGen) addMethod(svc protoreflect.ServiceDescriptor, m protoreflect.MethodDescriptor, tag string) (bool, error) {
	opts := m.Options()
	if opts == nil || !proto.HasExtension(opts, annotations.E_Http) {
		return false, nil
	}
	// gRPC-gateway style streaming is not representable as a JSON operation.
	if m.IsStreamingClient() || m.IsStreamingServer() {
		return false, nil
	}

	var oo *pckt.OperationOptions
	if proto.HasExtension(opts, pckt.E_Operation) {
		oo = proto.GetExtension(opts, pckt.E_Operation).(*pckt.OperationOptions)
	}
	if oo.GetHidden() {
		return false, nil
	}

	rule := proto.GetExtension(opts, annotations.E_Http).(*annotations.HttpRule)
	rules := append([]*annotations.HttpRule{rule}, rule.GetAdditionalBindings()...)

	baseID := string(svc.Name()) + "_" + string(m.Name())
	if oo.GetOperationId() != "" {
		baseID = oo.GetOperationId()
	}

	for i, r := range rules {
		op, err := g.operation(m, r)
		if err != nil {
			return false, fmt.Errorf("%s: %w", m.FullName(), err)
		}

		op.OperationID = baseID
		if i > 0 {
			op.OperationID += "_" + strconv.Itoa(i)
		}
		op.Tags = []string{tag}
		op.Summary, op.Description = splitSummary(comments(m))
		if mo, ok := opts.(interface{ GetDeprecated() bool }); ok {
			op.Deprecated = mo.GetDeprecated()
		}
		if oo != nil {
			if oo.GetSummary() != "" {
				op.Summary = oo.GetSummary()
			}
			if len(oo.GetTags()) > 0 {
				op.Tags = oo.GetTags()
			}
			if oo.GetDeprecated() {
				op.Deprecated = true
			}
			op.Extensions = g.extensions(m.FullName(), oo.GetExtensions())
		}

		method, tmpl := httpPattern(r)
		if method == "" {
			return false, fmt.Errorf("%s: google.api.http rule has no pattern", m.FullName())
		}
		path := pathVarRe.ReplaceAllString(tmpl, "{$1}")

		item := g.doc.Paths[path]
		if item == nil {
			item = &openapi.PathItem{}
			g.doc.Paths[path] = item
		}
		slot := item.Operation(method)
		if slot == nil {
			return false, fmt.Errorf("%s: unsupported HTTP method %q", m.FullName(), method)
		}
		if *slot != nil {
			return false, fmt.Errorf("%s: %s %s is already bound to %s", m.FullName(), strings.ToUpper(method), path, (*slot).OperationID)
		}
		*slot = op
	}

	return true, nil
}

// httpPattern returns the lowercase HTTP method and the path template of a rule.
func httpPattern(r *annotations.HttpRule) (string, string) {
	switch p := r.GetPattern().(type) {
	case *annotations.HttpRule_Get:
		return "get", p.Get
	case *annotations.HttpRule_Put:
		return "put", p.Put
	case *annotations.HttpRule_Post:
		return "post", p.Post
	case *annotations.HttpRule_Delete:
		return "delete", p.Delete
	case *annotations.HttpRule_Patch:
		return "patch", p.Patch
	case *annotations.HttpRule_Custom:
		return strings.ToLower(p.Custom.GetKind()), p.Custom.GetPath()
	}
	return "", ""
}

// operation builds the parameters, request body and responses of a binding.
func (g *fileGen) operation(m protoreflect.MethodDescriptor, r *annotations.HttpRule) (*openapi.Operation, error) {
	input, output := m.Input(), m.Output()
	op := &openapi.Operation{}

	_, tmpl := httpPattern(r)

	// Path parameters.
	inPath := map[string]bool{}
	for _, match := range pathVarRe.FindAllStringSubmatch(tmpl, -1) {
		name := match[1]
		f := fieldByPath(input, name)
		if f == nil {
			return nil, fmt.Errorf("path variable %q is not a field of %s", name, input.FullName())
		}
		inPath[name] = true

		s := g.fieldSchema(f)
		param := &openapi.Parameter{
			Name:        name,
			In:          "path",
			Required:    true,
			Description: s.Description,
			Deprecated:  s.Deprecated,
			Schema:      s,
		}
		s.Description, s.Deprecated, s.ReadOnly, s.WriteOnly = "", false, false, false
		if p := segmentsPattern(match[2]); p != "" {
			s.Pattern = p
		}
		op.Parameters = append(op.Parameters, param)
	}

	// Request body.
	body := r.GetBody()
	switch body {
	case "":
	case "*":
		var s *openapi.Schema
		if hasTopLevel(inPath) {
			s = g.messageSchema(input, func(f protoreflect.FieldDescriptor) bool {
				return inPath[string(f.Name())]
			})
		} else {
			s = g.messageRef(input)
		}
		op.RequestBody = &openapi.RequestBody{
			Required: true,
			Content:  map[string]*openapi.MediaType{jsonContentType: {Schema: s}},
		}
	default:
		f := fieldByPath(input, body)
		if f == nil {
			return nil, fmt.Errorf("body %q is not a field of %s", body, input.FullName())
		}
		s := g.fieldSchema(f)
		op.RequestBody = &openapi.RequestBody{
			Description: s.Description,
			Required:    true,
			Content:     map[string]*openapi.MediaType{jsonContentType: {Schema: s}},
		}
		s.Description = ""
	}

	// Query parameters: every field neither in the path nor in the body.
	if body != "*" {
		exclude := map[string]bool{}
		for name := range inPath {
			exclude[name] = true
		}
		if body != "" {
			exclude[body] = true
		}
		op.Parameters = append(op.Parameters, g.queryParams(input, "", "", exclude, map[protoreflect.FullName]bool{input.FullName(): true})...)
	}

	// Responses.
	var resp *openapi.Schema
	if rb := r.GetResponseBody(); rb != "" {
		f := fieldByPath(output, rb)
		if f == nil {
			return nil, fmt.Errorf("response_body %q is not a field of %s", rb, output.FullName())
		}
		resp = g.fieldSchema(f)
	} else {
		resp = g.messageRef(output)
	}

	g.schemas[statusSchemaName] = statusSchema()
	op.Responses = map[string]*openapi.Response{
		"200": {
			Description: "A successful response.",
			Content:     map[string]*openapi.MediaType{jsonContentType: {Schema: resp}},
		},
		"default": {
			Description: "An unexpected error response.",
			Content:     map[string]*openapi.MediaType{jsonContentType: {Schema: ref(statusSchemaName)}},
		},
	}

	return op, nil
}

// segmentsPattern converts the segments of a path variable (`shelves/*/books/*`)
// to the regular expression its value must match. The default `*` segment
// gives no pattern.
func segmentsPattern(segments string) string {
	if segments == "" || segments == "*" {
		return ""
	}
	parts := strings.Split(segments, "/")
	for i, part := range parts {
		switch part {
		case "*":
			parts[i] = "[^/]+"
		case "**":
			parts[i] = ".+"
		default:
			parts[i] = regexp.QuoteMeta(part)
		}
	}
	return "^" + strings.Join(parts, "/") + "$"
}

func hasTopLevel(paths map[string]bool) bool {
	for p := range paths {
		if !strings.Contains(p, ".") {
			return true
		}
	}
	return false
}

// queryParams flattens the fields of a message into query parameters, nested
// messages being expanded as `parent.child`.
func (g *fileGen) queryParams(m protoreflect.MessageDescriptor, protoPrefix, jsonPrefix string, exclude map[string]bool, visiting map[protoreflect.FullName]bool) []*openapi.Parameter {
	var params []*openapi.Parameter

	fields := m.Fields()
	for i := range fields.Len() {
		f := fields.Get(i)
		protoName := protoPrefix + string(f.Name())
		jsonName := jsonPrefix + f.JSONName()

		if exclude[protoName] || fieldHidden(f) || f.IsMap() {
			continue
		}

		if msg := f.Message(); msg != nil {
			wkt := wellKnownSchema(msg)
			if wkt == nil {
				if f.IsList() || visiting[msg.FullName()] || strings.Count(protoName, ".") >= maxQueryDepth {
					continue
				}
				visiting[msg.FullName()] = true
				params = append(params, g.queryParams(msg, protoName+".", jsonName+".", exclude, visiting)...)
				delete(visiting, msg.FullName())
				continue
			}
			if !isScalarType(wkt) {
				continue
			}
		}

		s := g.fieldSchema(f)
		param := &openapi.Parameter{
			Name:        jsonName,
			In:          "query",
			Description: s.Description,
			Deprecated:  s.Deprecated,
			Required:    fieldRequired(f),
			Schema:      s,
		}
		s.Description, s.Deprecated, s.ReadOnly, s.WriteOnly = "", false, false, false
		params = append(params, param)
	}

	return params
}

func isScalarType(s *openapi.Schema) bool {
	if len(s.Type) == 0 {
		return false
	}
	switch s.Type[0] {
	case "string", "number", "integer", "boolean":
		return true
	}
	return false
}
