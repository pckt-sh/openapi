# pckt-openapi

[![CI](https://github.com/pckt-sh/openapi/actions/workflows/ci.yml/badge.svg)](https://github.com/pckt-sh/openapi/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/pckt-sh/openapi.svg)](https://pkg.go.dev/github.com/pckt-sh/openapi)
[![Buf](https://img.shields.io/badge/buf.build-pckt%2Fopenapi-blue)](https://buf.build/pckt/openapi)

Generate OpenAPI 3.1 documents from Protocol Buffers services annotated with
[`google.api.http`](https://github.com/googleapis/googleapis/blob/master/google/api/http.proto).

| Component                 | Role                                                                    |
| ------------------------- | ----------------------------------------------------------------------- |
| `protoc-gen-pckt-openapi` | protoc / buf plugin: one OpenAPI document per `.proto` file, or a single merged document. |
| `pckt-openapi-merge`      | CLI merging OpenAPI documents (YAML or JSON) into one.                   |
| `buf.build/pckt/openapi`  | Optional annotations (`pckt/openapi/annotations.proto`) to enrich the output. |

## Install

```sh
go install github.com/pckt-sh/openapi/cmd/protoc-gen-pckt-openapi@latest
go install github.com/pckt-sh/openapi/cmd/pckt-openapi-merge@latest
```

Prebuilt binaries for Linux, macOS and Windows are attached to
[GitHub releases](https://github.com/pckt-sh/openapi/releases).

## Usage with buf

`buf.yaml` of your project (the annotations dependency is only needed if you use them):

```yaml
version: v2
deps:
  - buf.build/googleapis/googleapis
  - buf.build/pckt/openapi
```

`buf.gen.yaml`, generating a **single merged document** in one step:

```yaml
version: v2
plugins:
  - local: protoc-gen-pckt-openapi
    out: gen/openapi
    strategy: all
    opt:
      - merge=api.yaml # .json for JSON
      - title=Shop API
      - version=1.0.0
      - server=https://api.pckt.sh|REST API # description after `|` is optional
      - server=https://grpc.pckt.sh|gRPC-Web
      - examples_dir=openapi/examples # see "Request and response examples"
```

> [!IMPORTANT]
> `strategy: all` is required with `merge`. By default buf invokes plugins once
> per directory: each invocation would write its own `api.yaml` and buf keeps
> only the first one (with a "duplicate generated file name" warning), giving an
> incomplete document.

Or **one document per `.proto` file**, to merge later with `pckt-openapi-merge`
(for instance with a `-base` document holding security schemes):

```yaml
version: v2
plugins:
  - local: protoc-gen-pckt-openapi
    out: gen/openapi
    opt:
      - format=yaml
```

```sh
pckt-openapi-merge -o api.yaml -base base.yaml -title "Shop API" -version 1.0.0 gen/openapi
```

Without installing the plugin, `local` can also run it through Go, pinned to a version:

```yaml
  - local: ["go", "run", "github.com/pckt-sh/openapi/cmd/protoc-gen-pckt-openapi@v0.1.0"]
```

If your project generates Go code with [managed mode](https://buf.build/docs/generate/managed-mode/),
keep the `go_package` of the annotations, whose Go code lives in this module
(`github.com/pckt-sh/openapi/proto/pckt/openapi`):

```yaml
managed:
  enabled: true
  disable:
    - file_option: go_package
      module: buf.build/pckt/openapi
```

## Usage with protoc

protoc sends every file given on the command line in a single request, so `merge` works directly:

```sh
buf export buf.build/googleapis/googleapis -o third_party
buf export buf.build/pckt/openapi -o third_party

protoc -I proto -I third_party \
  --pckt-openapi_out=merge=api.yaml,title=Shop\ API,version=1.0.0:gen/openapi \
  $(find proto -name '*.proto')
```

### Plugin options

Options are `key=value` pairs, separated by commas with protoc or given as `opt` entries with buf.

| Option    | Default                     | Description                                                                 |
| --------- | --------------------------- | --------------------------------------------------------------------------- |
| `format`  | `yaml`                      | `yaml` or `json`, per-file output is `<file>.openapi.<format>`              |
| `merge`   |                             | write a single document at this path instead of one per file, `.json` for JSON, YAML otherwise |
| `title`   | proto path, `API` if merged | `info.title`                                                                |
| `version` | `0.0.0`                     | `info.version`                                                              |
| `server`  |                             | server of the merged document, `URL` or `URL\|description`, can be repeated  |
| `examples_dir` | working directory      | directory of example files, relative to where `buf`/`protoc` runs          |

Files without HTTP operations, messages nor enums produce no output.
`merge` applies the same [rules](#merge-rules) as `pckt-openapi-merge`.

### `pckt-openapi-merge` flags

```
pckt-openapi-merge [flags] <files or dirs...>
  -o            output file, .json for JSON, YAML otherwise, - (default) for stdout
  -base         base document merged first: info, servers, security, securitySchemes, x-*...
  -title        info.title
  -version      info.version
  -description  info.description
  -server       server URL[|description], can be repeated
```

Directories are walked for `*.openapi.yaml`, `*.openapi.yml` and `*.openapi.json`,
files given explicitly are read whatever their name. Use the CLI rather than the
`merge` option when you need a `-base` document, or to merge documents coming
from several generation runs.

## Annotations

Import `pckt/openapi/annotations.proto` from the buf module `buf.build/pckt/openapi`
(sources in [`proto/`](proto/)).

**Descriptions always come from leading comments** of the annotated element
(message, field, enum, enum value, method, service); there is no description option.
For methods, the description is the full comment; the summary is only set by
the `summary` option of `(pckt.openapi.operation)`.

```proto
// A book in a shelf.
message Book {
  option (pckt.openapi.schema) = {
    title: "Book"
    example: "{\"title\": \"Dune\"}"
    extensions: {key: "x-resource" value: {string_value: "Book"}}
  };

  // Contact email of the author.
  string author_email = 3 [(pckt.openapi.field) = {
    format: FIELD_FORMAT_EMAIL
    example: "jane@example.com"
  }];

  // Exposed as a JSON number instead of the protojson string.
  int64 sales = 5 [(pckt.openapi.field) = {type: FIELD_TYPE_INT64}];

  string internal_note = 13 [(pckt.openapi.field) = {hidden: true}];
}

// Greetings
service HelloService {
  option (pckt.openapi.tag) = {name: "Hello"};

  // Say hello.
  //
  // Returns a personalized greeting.
  rpc Hello(HelloRequest) returns (HelloResponse) {
    option (google.api.http) = {get: "/v1/hello"};
    option (pckt.openapi.operation) = {
      operation_id: "sayHello"
      extensions: {key: "x-rate-limit" value: {number_value: 10}}
    };
  }
}
```

| Extension                  | Options                                                                                                   |
| -------------------------- | --------------------------------------------------------------------------------------------------------- |
| `(pckt.openapi.field)`     | `example`, `type`, `format`, `pattern`, `deprecated`, `hidden`, `required`, `read_only`, `write_only`, `minimum`, `maximum`, `min_length`, `max_length`, `min_items`, `max_items`, `extensions` |
| `(pckt.openapi.schema)`    | `title`, `type`, `hidden`, `example` (JSON), `deprecated`, `extensions`                                    |
| `(pckt.openapi.operation)` | `summary`, `tags`, `operation_id`, `deprecated`, `hidden`, `extensions`, `request_example`, `response_example`, `request_content_type`, `response_content_type` |
| `(pckt.openapi.tag)`       | `name`, `external_docs`                                                                                   |

- `example` is a string: for string schemas it is used literally (`"John"` →
  `John`), otherwise it is parsed as JSON (`"20"` → `20`).
- `extensions` is a `map<string, google.protobuf.Value>`; keys must start with
  `x-`, generation fails otherwise. They are written inline in the schema or operation.
- `hidden` on a message also hides every field of that message type.
- The standard `deprecated` option and `google.api.field_behavior`
  (`REQUIRED` → `required`, `OUTPUT_ONLY` → `readOnly`, `INPUT_ONLY` → `writeOnly`) are honored too.

### Request and response examples

Examples of a method's request body and successful response are set on the
operation, not on the schema, so methods sharing a message (or
`google.api.HttpBody`) each get their own example. Big examples live in files:

```proto
rpc GetItem(GetItemRequest) returns (Item) {
  option (google.api.http) = {get: "/v1/items/{id}"};
  option (pckt.openapi.operation) = {
    response_example: {file: "shop/get_item.json"}
  };
}

rpc ExportItems(ExportItemsRequest) returns (google.api.HttpBody) {
  option (google.api.http) = {get: "/v1/items:export"};
  option (pckt.openapi.operation) = {
    response_content_type: "text/csv"
    response_example: {file: "shop/export.csv"}
  };
}

rpc CreateItem(CreateItemRequest) returns (Item) {
  option (google.api.http) = {post: "/v1/items" body: "item"};
  option (pckt.openapi.operation) = {
    request_example: {value: "{\"name\": \"Mug\", \"price\": 990}"}
  };
}
```

- `file` is relative to the `examples_dir` plugin option, which is itself
  relative to the directory `buf generate` or `protoc` runs in. Files cannot
  point outside of `examples_dir`; a missing or invalid file fails generation.
- For JSON content types (`application/json`, `*+json`), the example is parsed:
  JSON, or YAML for `.yaml`/`.yml` files. Key order is kept. For other content
  types it is used as text.
- `request_example` applies to the bindings with a body; setting it on a method
  without any is an error.

## Mapping rules

### Types (canonical protojson)

| Proto                                   | Schema                                              |
| --------------------------------------- | --------------------------------------------------- |
| `bool`                                  | `boolean`                                           |
| `int32`, `sint32`, `sfixed32`           | `integer` / `int32`                                 |
| `uint32`, `fixed32`                     | `integer` / `int64`, `minimum: 0`                   |
| `int64`, `sint64`, `sfixed64`           | `string` / `int64`                                  |
| `uint64`, `fixed64`                     | `string` / `uint64`                                 |
| `float`, `double`                       | `number` / `float`, `double`                        |
| `string`                                | `string`                                            |
| `bytes`                                 | `string` / `byte`                                   |
| enum                                    | `$ref` to a `string` schema with value names        |
| message                                 | `$ref` to `#/components/schemas/<full.proto.Name>`  |
| `repeated T`                            | `array` of `T`                                      |
| `map<K, V>`                             | `object` with `additionalProperties: V`             |
| `Timestamp`                             | `string` / `date-time`                              |
| `Duration`                              | `string` with a `^-?[0-9]+(\.[0-9]{1,9})?s$` pattern |
| `FieldMask`                             | `string`                                            |
| `Struct`, `Value`, `ListValue`, `Empty` | `object`, any, `array`, `object`                    |
| `Any`                                   | `object` with `@type`                               |
| wrappers (`Int32Value`...)              | the scalar type plus `null`                         |

Property names are the JSON names (`lowerCamelCase`). Schemas are keyed by the
fully qualified proto name, so names are unique across packages and identical
schemas from different files deduplicate on merge.

### HTTP

Only methods with a `google.api.http` rule are emitted (streaming methods are
skipped). Every binding, including `additional_bindings`, becomes an operation;
additional ones get an `_N` suffix on their operation ID (`Service_Method_1`).

- **Path**: `{name=shelves/*/books/*}` becomes `{name}`, the segments are kept
  as a `pattern` on the parameter. Nested variables (`{book.name}`) are supported.
- **Body**: `body: "*"` sends the request message (minus path parameters, as an
  inline schema); `body: "field"` sends that field.
- **Query**: every other field, nested messages flattened as `parent.child`
  (up to 5 levels, no cycles); maps and repeated messages are not representable and skipped.
- **Responses**: `200` with the response message (or `response_body` field),
  `default` with `google.rpc.Status`.
- **Content types**: `application/json`, overridable with `request_content_type`
  and `response_content_type`.
- **`google.api.HttpBody`**: as a request (`body: "*"` on an HttpBody input, or a
  `body` field of that type) or response (output or `response_body` field), it is
  a raw body as served by grpc-gateway: `string`/`binary` schema, content type
  `application/octet-stream` unless set with the content type options.

## Merge rules

`pckt-openapi-merge` and the `merge` plugin option are strict, so that a merged
document never silently loses an operation or a schema:

- **paths**: operations are merged per path and method. The same path and
  method defined by two inputs is an error, unless identical.
- **components**: every component (`schemas`, `securitySchemes`...) is keyed by
  name; identical definitions are kept once, different ones are an error naming both files.
- **operationId**: must be unique in the merged document.
- **tags**: union by name, in input order; the first non-empty description wins (a warning is printed on conflict).
- **servers**: union of the base document, inputs and `-server` flags.
- **info**: from `-base` and flags only. Per-file `info` is generated and ignored.
- **other top-level fields** (`security`, `x-*`...): kept, must be identical across inputs.

Paths and component entries are sorted by key, so the output does not depend on the input order.

## Internals

```
cmd/protoc-gen-pckt-openapi  plugin entrypoint: stdin request → stdout response
cmd/pckt-openapi-merge       merge CLI: flags, file collection, output
internal/gen                 the generator
  gen.go                     options parsing, Run(), per-file loop, merge option, encoding
  file.go                    fileGen: services, google.api.http → operations and parameters
  examples.go                request/response examples, content types, google.api.HttpBody
  schema.go                  messages, fields, enums, well-known types → schemas; annotations
  comments.go                leading comment cleaning
internal/openapi             the OpenAPI 3.1 document model written by the generator
  node.go                    ordered JSON values (yaml.Node) and their JSON encoding
internal/merge               the merger, shared by the CLI and the merge option
  merge.go                   merge of yaml.Node documents, conflict detection, YAML encoding
  json.go                    yaml.Node → ordered JSON encoding
proto/                       the buf.build/pckt/openapi module (README.md is its BSR page)
  pckt/openapi               annotation definitions and their generated Go code
testdata/proto               example protos covering the features
testdata/golden              expected generator (YAML and JSON) and merge outputs
```

### Generator

`cmd/protoc-gen-pckt-openapi` reads a `CodeGeneratorRequest` and calls `gen.Run`, which:

1. builds a `protoregistry.Files` from every file of the request with
   `protodesc.NewFiles`. The plugin does **not** use `protogen`: it requires a
   `go_package` for every file, which is meaningless for OpenAPI;
2. for each file to generate, runs a `fileGen` that walks services and methods
   (`file.go`), then every message and enum of the file, so files holding only
   shared types still produce components;
3. encodes the document and returns `<file>.openapi.<format>`; with the
   `merge` option, the per-file documents are instead passed to
   `internal/merge` and only the merged document is returned.

Annotation options are read with `proto.GetExtension` on the descriptor
options. This works because importing the generated Go packages
(`proto/pckt/openapi`, `google.golang.org/genproto/googleapis/api/annotations`)
registers the extension types, so `proto.Unmarshal` of the request decodes them.

`fileGen.messageRef` adds a message schema to `components.schemas` and returns
a `$ref`. It first registers a placeholder, so recursive messages terminate,
and recursively adds every message reachable from fields, including messages of
imported files: each document is self-contained, the merger deduplicates them.
Well-known types are inlined instead of referenced.

Example files are read through an `os.Root` opened on `examples_dir` on first
use: the directory only has to exist when examples are used, and paths with
`..` or absolute paths cannot escape it. Examples, like annotation `example`
values, are parsed into ordered `yaml.Node` trees (`openapi.Node`) instead of
Go maps, so keys keep the order they were written in, in YAML and JSON output.

Annotation errors (e.g. an extension key without `x-`) are collected in
`fileGen.errs` while building schemas, and returned at the end of the file, so
all of them are reported at once.

The document model (`internal/openapi`) is a small set of structs written for
the generator instead of a third-party one: it needs `title`, `readOnly`,
`writeOnly`, 3.1 type lists (`[number, "null"]`), inline `x-*` extensions in
both formats, and properties kept in proto field order (`Properties` is an
ordered slice with custom YAML and JSON marshalers). `Schema` and `Operation`
have a `MarshalJSON` appending extensions to the object; YAML uses `,inline`.

### Merger

The merger does not decode documents into structs: each document is parsed as
a `yaml.Node` tree (JSON is valid YAML). Working on nodes keeps key order and
every field, including ones the tool does not know about (`security`,
`callbacks`, `x-*`...). Equality between two definitions is checked by decoding
both nodes to Go values and comparing them, so formatting and quoting differences
between YAML and JSON inputs do not matter. `owners` records which input
defined each location, to name both files in conflict errors.

Before output, styles inherited from JSON inputs (flow mappings, quoted
scalars) are reset so the YAML encoder chooses its own; JSON output is written
by walking the node tree in order (`json.go`).

## Development

Requires Go (see `go.mod`), [buf](https://buf.build/docs/installation) and [Task](https://taskfile.dev).

```sh
task build-annotations   # regenerate Go code of the annotations
task test                # unit tests, then generate and merge testdata into testdata/generated
task golden              # after an intended output change: rebuild the test descriptor set, update goldens
```

The generator tests run the plugin in-process on `testdata/descriptor.binpb`
(built from `testdata/proto` with `buf build`) and compare against
`testdata/golden`, in YAML and JSON. Every generated and merged document is
also parsed with [ogen](https://github.com/ogen-go/ogen) to check it is a
usable spec. **Run `task golden` after changing protos** under `testdata` or
`proto`, otherwise tests use a stale descriptor set. `task test` also runs buf
with both strategies (`testdata/buf.gen.yaml`) and checks the plugin `merge`
option and `pckt-openapi-merge` produce the same document.

CI (`.github/workflows`) runs the tests, `gofmt`, `buf lint`, `buf format`,
`buf breaking` on pull requests, and checks generated code and the descriptor
set are up to date.

### Releasing

Push a `v*` tag:

- `release.yml` builds binaries with GoReleaser and publishes a GitHub release;
- `buf-push.yml` pushes the `proto/` module to `buf.build/pckt/openapi`
  (labelled with the tag; pushes on `main` update the `main` label). It requires
  a `BUF_TOKEN` repository secret, a BSR token with write access to the `pckt` organization.

## License

[Apache-2.0](LICENSE)

## Limitations

- Path templates with patterns are collapsed to `{var}`: two rules differing
  only by the variable pattern (`/v1/{name=shelves/*}` and
  `/v1/{name=shelves/*/books/*}`) map to the same OpenAPI path and conflict.
- Streaming methods are not emitted.
- Path parameter values may contain `/` (e.g. `shelves/1/books/2`), which
  OpenAPI tooling does not always support; the `pattern` documents it.
