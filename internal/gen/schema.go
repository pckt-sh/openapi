package gen

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/pckt-sh/openapi/internal/openapi"
	pckt "github.com/pckt-sh/openapi/proto/pckt/openapi"
)

const refPrefix = "#/components/schemas/"

const statusSchemaName = "google.rpc.Status"

var fieldFormats = map[pckt.FieldFormat]string{
	pckt.FieldFormat_FIELD_FORMAT_DATE_TIME:             "date-time",
	pckt.FieldFormat_FIELD_FORMAT_TIME:                  "time",
	pckt.FieldFormat_FIELD_FORMAT_DATE:                  "date",
	pckt.FieldFormat_FIELD_FORMAT_DURATION:              "duration",
	pckt.FieldFormat_FIELD_FORMAT_EMAIL:                 "email",
	pckt.FieldFormat_FIELD_FORMAT_IDN_EMAIL:             "idn-email",
	pckt.FieldFormat_FIELD_FORMAT_HOSTNAME:              "hostname",
	pckt.FieldFormat_FIELD_FORMAT_IDN_HOSTNAME:          "idn-hostname",
	pckt.FieldFormat_FIELD_FORMAT_IPV4:                  "ipv4",
	pckt.FieldFormat_FIELD_FORMAT_IPV6:                  "ipv6",
	pckt.FieldFormat_FIELD_FORMAT_UUID:                  "uuid",
	pckt.FieldFormat_FIELD_FORMAT_URI:                   "uri",
	pckt.FieldFormat_FIELD_FORMAT_URI_REFERENCE:         "uri-reference",
	pckt.FieldFormat_FIELD_FORMAT_IRI:                   "iri",
	pckt.FieldFormat_FIELD_FORMAT_IRI_REFERENCE:         "iri-reference",
	pckt.FieldFormat_FIELD_FORMAT_URI_TEMPLATE:          "uri-template",
	pckt.FieldFormat_FIELD_FORMAT_JSON_POINTER:          "json-pointer",
	pckt.FieldFormat_FIELD_FORMAT_RELATIVE_JSON_POINTER: "relative-json-pointer",
	pckt.FieldFormat_FIELD_FORMAT_REGEX:                 "regex",
}

func fieldOptions(f protoreflect.FieldDescriptor) *pckt.FieldOptions {
	opts := f.Options()
	if opts == nil || !proto.HasExtension(opts, pckt.E_Field) {
		return nil
	}
	return proto.GetExtension(opts, pckt.E_Field).(*pckt.FieldOptions)
}

func schemaOptions(m protoreflect.MessageDescriptor) *pckt.SchemaOptions {
	opts := m.Options()
	if opts == nil || !proto.HasExtension(opts, pckt.E_Schema) {
		return nil
	}
	return proto.GetExtension(opts, pckt.E_Schema).(*pckt.SchemaOptions)
}

func fieldBehaviors(f protoreflect.FieldDescriptor) []annotations.FieldBehavior {
	opts := f.Options()
	if opts == nil || !proto.HasExtension(opts, annotations.E_FieldBehavior) {
		return nil
	}
	return proto.GetExtension(opts, annotations.E_FieldBehavior).([]annotations.FieldBehavior)
}

func hasBehavior(f protoreflect.FieldDescriptor, b annotations.FieldBehavior) bool {
	return slices.Contains(fieldBehaviors(f), b)
}

// fieldHidden reports whether a field is omitted, either directly or because
// the message it holds is hidden.
func fieldHidden(f protoreflect.FieldDescriptor) bool {
	if fieldOptions(f).GetHidden() {
		return true
	}
	if f.IsMap() {
		f = f.MapValue()
	}
	if m := f.Message(); m != nil {
		return schemaOptions(m).GetHidden()
	}
	return false
}

func fieldRequired(f protoreflect.FieldDescriptor) bool {
	return fieldOptions(f).GetRequired() || hasBehavior(f, annotations.FieldBehavior_REQUIRED)
}

// extensions converts annotation extensions, rejecting keys without the `x-` prefix.
func (g *fileGen) extensions(owner protoreflect.FullName, values map[string]*structpb.Value) openapi.Extensions {
	if len(values) == 0 {
		return nil
	}
	ext := openapi.Extensions{}
	for key, value := range values {
		if !strings.HasPrefix(key, "x-") {
			g.errs = append(g.errs, fmt.Errorf("%s: extension %q must start with \"x-\"", owner, key))
			continue
		}
		ext[key] = value.AsInterface()
	}
	return ext
}

func ref(name string) *openapi.Schema {
	return &openapi.Schema{Ref: refPrefix + name}
}

// messageRef returns a $ref to the message, adding its schema (and the
// schemas it depends on) to the components. Well-known types are inlined.
func (g *fileGen) messageRef(m protoreflect.MessageDescriptor) *openapi.Schema {
	if s := wellKnownSchema(m); s != nil {
		return s
	}

	name := string(m.FullName())
	if _, ok := g.schemas[name]; !ok {
		// Register a placeholder first so recursive messages terminate.
		placeholder := &openapi.Schema{}
		g.schemas[name] = placeholder
		*placeholder = *g.messageSchema(m, nil)
	}
	return ref(name)
}

func (g *fileGen) enumRef(e protoreflect.EnumDescriptor) *openapi.Schema {
	if e.FullName() == "google.protobuf.NullValue" {
		return &openapi.Schema{Type: openapi.Types{"null"}}
	}

	name := string(e.FullName())
	if _, ok := g.schemas[name]; !ok {
		s := &openapi.Schema{Type: openapi.Types{"string"}}
		desc := []string{}
		if c := comments(e); c != "" {
			desc = append(desc, c)
		}
		values := e.Values()
		for i := range values.Len() {
			v := values.Get(i)
			s.Enum = append(s.Enum, string(v.Name()))
			if c := comments(v); c != "" {
				desc = append(desc, "- `"+string(v.Name())+"`: "+strings.ReplaceAll(c, "\n", " "))
			}
		}
		s.Description = strings.Join(desc, "\n")
		if opts, ok := e.Options().(interface{ GetDeprecated() bool }); ok {
			s.Deprecated = opts.GetDeprecated()
		}
		g.schemas[name] = s
	}
	return ref(name)
}

// messageSchema builds the object schema of a message, omitting fields for
// which skip returns true.
func (g *fileGen) messageSchema(m protoreflect.MessageDescriptor, skip func(protoreflect.FieldDescriptor) bool) *openapi.Schema {
	s := &openapi.Schema{Type: openapi.Types{"object"}}

	fields := m.Fields()
	for i := range fields.Len() {
		f := fields.Get(i)
		if fieldHidden(f) || (skip != nil && skip(f)) {
			continue
		}
		s.Properties = append(s.Properties, openapi.Property{Name: f.JSONName(), Schema: g.fieldSchema(f)})
		if fieldRequired(f) {
			s.Required = append(s.Required, f.JSONName())
		}
	}

	s.Description = comments(m)
	if opts, ok := m.Options().(interface{ GetDeprecated() bool }); ok {
		s.Deprecated = opts.GetDeprecated()
	}

	if so := schemaOptions(m); so != nil {
		if so.GetTitle() != "" {
			s.Title = so.GetTitle()
		}
		if so.GetDeprecated() {
			s.Deprecated = true
		}
		if so.GetType() != pckt.FieldType_FIELD_TYPE_UNSPECIFIED {
			applyType(s, so.GetType())
		}
		if so.GetExample() != "" {
			s.Examples = []any{parseExample(so.GetExample(), "object")}
		}
		s.Extensions = g.extensions(m.FullName(), so.GetExtensions())
	}

	return s
}

// fieldSchema builds the schema of a field, including annotations.
func (g *fileGen) fieldSchema(f protoreflect.FieldDescriptor) *openapi.Schema {
	fo := fieldOptions(f)

	var s *openapi.Schema
	switch {
	case f.IsMap():
		value := g.kindSchema(f.MapValue())
		applyValueOptions(value, fo)
		s = &openapi.Schema{Type: openapi.Types{"object"}, AdditionalProperties: value}
	case f.IsList():
		item := g.kindSchema(f)
		applyValueOptions(item, fo)
		s = &openapi.Schema{Type: openapi.Types{"array"}, Items: item}
		if fo.HasMinItems() {
			s.MinItems = proto.Uint64(fo.GetMinItems())
		}
		if fo.HasMaxItems() {
			s.MaxItems = proto.Uint64(fo.GetMaxItems())
		}
	default:
		s = g.kindSchema(f)
		applyValueOptions(s, fo)
	}

	s.Description = comments(f)
	if opts, ok := f.Options().(interface{ GetDeprecated() bool }); ok && opts.GetDeprecated() {
		s.Deprecated = true
	}
	s.ReadOnly = hasBehavior(f, annotations.FieldBehavior_OUTPUT_ONLY)
	s.WriteOnly = hasBehavior(f, annotations.FieldBehavior_INPUT_ONLY)

	if fo != nil {
		if fo.GetDeprecated() {
			s.Deprecated = true
		}
		if fo.GetReadOnly() {
			s.ReadOnly = true
		}
		if fo.GetWriteOnly() {
			s.WriteOnly = true
		}
		if fo.GetExample() != "" {
			kind := "string"
			if len(s.Type) > 0 {
				kind = s.Type[0]
			}
			s.Examples = []any{parseExample(fo.GetExample(), kind)}
		}
		s.Extensions = g.extensions(f.FullName(), fo.GetExtensions())
	}

	return s
}

// applyValueOptions applies the annotations constraining a single value
// (the field itself, or the items/values of a repeated/map field).
func applyValueOptions(s *openapi.Schema, fo *pckt.FieldOptions) {
	if fo == nil {
		return
	}
	if fo.GetType() != pckt.FieldType_FIELD_TYPE_UNSPECIFIED {
		applyType(s, fo.GetType())
	}
	if f, ok := fieldFormats[fo.GetFormat()]; ok {
		s.Format = f
	}
	if fo.GetPattern() != "" {
		s.Pattern = fo.GetPattern()
	}
	if fo.HasMinimum() {
		s.Minimum = proto.Float64(fo.GetMinimum())
	}
	if fo.HasMaximum() {
		s.Maximum = proto.Float64(fo.GetMaximum())
	}
	if fo.HasMinLength() {
		s.MinLength = proto.Uint64(fo.GetMinLength())
	}
	if fo.HasMaxLength() {
		s.MaxLength = proto.Uint64(fo.GetMaxLength())
	}
}

// applyType overrides the inferred type, e.g. to expose an int64 as a JSON number.
func applyType(s *openapi.Schema, t pckt.FieldType) {
	switch t {
	case pckt.FieldType_FIELD_TYPE_FLOAT:
		s.Type, s.Format = openapi.Types{"number"}, "float"
	case pckt.FieldType_FIELD_TYPE_DOUBLE:
		s.Type, s.Format = openapi.Types{"number"}, "double"
	case pckt.FieldType_FIELD_TYPE_INT32:
		s.Type, s.Format = openapi.Types{"integer"}, "int32"
	case pckt.FieldType_FIELD_TYPE_INT64:
		s.Type, s.Format = openapi.Types{"integer"}, "int64"
	case pckt.FieldType_FIELD_TYPE_ARRAY:
		s.Type = openapi.Types{"array"}
	case pckt.FieldType_FIELD_TYPE_OBJECT:
		s.Type = openapi.Types{"object"}
	}
}

// parseExample converts an annotation example to a typed value: JSON is used
// as is, except for string schemas where unquoted values are taken literally.
func parseExample(example, kind string) any {
	if kind == "string" && !strings.HasPrefix(strings.TrimSpace(example), `"`) {
		return example
	}
	var v any
	dec := json.NewDecoder(strings.NewReader(example))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil || dec.More() {
		return example
	}
	return normalizeNumbers(v)
}

// normalizeNumbers converts json.Number values to int64 or float64, so that
// they are encoded as numbers in YAML too.
func normalizeNumbers(v any) any {
	switch v := v.(type) {
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return i
		}
		if f, err := v.Float64(); err == nil {
			return f
		}
		return v.String()
	case map[string]any:
		for k, e := range v {
			v[k] = normalizeNumbers(e)
		}
	case []any:
		for i, e := range v {
			v[i] = normalizeNumbers(e)
		}
	}
	return v
}

// kindSchema returns the schema of a single value of the field kind, following
// the protojson mapping.
func (g *fileGen) kindSchema(f protoreflect.FieldDescriptor) *openapi.Schema {
	switch f.Kind() {
	case protoreflect.BoolKind:
		return &openapi.Schema{Type: openapi.Types{"boolean"}}
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return &openapi.Schema{Type: openapi.Types{"integer"}, Format: "int32"}
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return &openapi.Schema{Type: openapi.Types{"integer"}, Format: "int64", Minimum: proto.Float64(0)}
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return &openapi.Schema{Type: openapi.Types{"string"}, Format: "int64"}
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return &openapi.Schema{Type: openapi.Types{"string"}, Format: "uint64"}
	case protoreflect.FloatKind:
		return &openapi.Schema{Type: openapi.Types{"number"}, Format: "float"}
	case protoreflect.DoubleKind:
		return &openapi.Schema{Type: openapi.Types{"number"}, Format: "double"}
	case protoreflect.StringKind:
		return &openapi.Schema{Type: openapi.Types{"string"}}
	case protoreflect.BytesKind:
		return &openapi.Schema{Type: openapi.Types{"string"}, Format: "byte"}
	case protoreflect.EnumKind:
		return g.enumRef(f.Enum())
	case protoreflect.MessageKind, protoreflect.GroupKind:
		return g.messageRef(f.Message())
	}
	return &openapi.Schema{}
}

// wellKnownSchema returns the inline protojson schema of well-known types,
// or nil for regular messages.
func wellKnownSchema(m protoreflect.MessageDescriptor) *openapi.Schema {
	nullable := func(t, format string) *openapi.Schema {
		return &openapi.Schema{Type: openapi.Types{t, "null"}, Format: format}
	}

	switch m.FullName() {
	case "google.protobuf.Timestamp":
		return &openapi.Schema{Type: openapi.Types{"string"}, Format: "date-time"}
	case "google.protobuf.Duration":
		return &openapi.Schema{Type: openapi.Types{"string"}, Pattern: `^-?[0-9]+(\.[0-9]{1,9})?s$`}
	case "google.protobuf.FieldMask":
		return &openapi.Schema{Type: openapi.Types{"string"}}
	case "google.protobuf.Empty":
		return &openapi.Schema{Type: openapi.Types{"object"}}
	case "google.protobuf.Struct":
		return &openapi.Schema{Type: openapi.Types{"object"}, AdditionalProperties: &openapi.Schema{}}
	case "google.protobuf.Value":
		return &openapi.Schema{}
	case "google.protobuf.ListValue":
		return &openapi.Schema{Type: openapi.Types{"array"}, Items: &openapi.Schema{}}
	case "google.protobuf.Any":
		return anySchema()
	case "google.protobuf.DoubleValue":
		return nullable("number", "double")
	case "google.protobuf.FloatValue":
		return nullable("number", "float")
	case "google.protobuf.Int64Value":
		return nullable("string", "int64")
	case "google.protobuf.UInt64Value":
		return nullable("string", "uint64")
	case "google.protobuf.Int32Value":
		return nullable("integer", "int32")
	case "google.protobuf.UInt32Value":
		return nullable("integer", "int64")
	case "google.protobuf.BoolValue":
		return nullable("boolean", "")
	case "google.protobuf.StringValue":
		return nullable("string", "")
	case "google.protobuf.BytesValue":
		return nullable("string", "byte")
	}
	return nil
}

func anySchema() *openapi.Schema {
	return &openapi.Schema{
		Type: openapi.Types{"object"},
		Properties: openapi.Properties{{
			Name:   "@type",
			Schema: &openapi.Schema{Type: openapi.Types{"string"}, Description: "Type URL of the serialized message."},
		}},
		AdditionalProperties: &openapi.Schema{},
	}
}

// statusSchema is the protojson schema of google.rpc.Status, used for error responses.
func statusSchema() *openapi.Schema {
	return &openapi.Schema{
		Type:        openapi.Types{"object"},
		Description: "The `Status` type defines a logical error model, see https://google.aip.dev/193.",
		Properties: openapi.Properties{
			{Name: "code", Schema: &openapi.Schema{Type: openapi.Types{"integer"}, Format: "int32", Description: "The status code, which should be an enum value of google.rpc.Code."}},
			{Name: "message", Schema: &openapi.Schema{Type: openapi.Types{"string"}, Description: "A developer-facing error message."}},
			{Name: "details", Schema: &openapi.Schema{Type: openapi.Types{"array"}, Items: anySchema(), Description: "A list of messages that carry the error details."}},
		},
	}
}

// fieldByPath resolves a dotted proto field path (`book.name`) from a message.
func fieldByPath(m protoreflect.MessageDescriptor, path string) protoreflect.FieldDescriptor {
	var f protoreflect.FieldDescriptor
	for i, part := range strings.Split(path, ".") {
		if i > 0 {
			if f == nil || f.Message() == nil || f.IsList() || f.IsMap() {
				return nil
			}
			m = f.Message()
		}
		f = m.Fields().ByName(protoreflect.Name(part))
		if f == nil {
			return nil
		}
	}
	return f
}
