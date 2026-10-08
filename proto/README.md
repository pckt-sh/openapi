# pckt/openapi

Annotations for [`protoc-gen-pckt-openapi`](https://github.com/pckt-sh/openapi),
a protoc / buf plugin generating OpenAPI 3.1 documents from services annotated
with `google.api.http`.

Annotations are optional: every message, field and HTTP-annotated method is
generated without them, and descriptions always come from leading comments.
They enrich the output with examples, formats, constraints, extensions, or hide
elements.

## Usage

`buf.yaml`:

```yaml
version: v2
deps:
  - buf.build/googleapis/googleapis
  - buf.build/pckt/openapi
```

```proto
syntax = "proto3";

package shop.v1;

import "google/api/annotations.proto";
import "pckt/openapi/annotations.proto";

// An item for sale.
message Item {
  option (pckt.openapi.schema) = {
    example: "{\"id\": \"3f0c...\", \"price\": 990}"
  };

  // Item identifier.
  string id = 1 [(pckt.openapi.field) = {format: FIELD_FORMAT_UUID}];

  // Price in cents.
  int64 price = 2 [(pckt.openapi.field) = {
    type: FIELD_TYPE_INT64
    minimum: 0
    extensions: {
      key: "x-unit"
      value: {string_value: "cent"}
    }
  }];

  string internal_note = 3 [(pckt.openapi.field) = {hidden: true}];
}

message GetItemRequest {
  string id = 1;
}

// Shop catalog.
service ShopService {
  option (pckt.openapi.tag) = {name: "Shop"};

  // Get an item.
  rpc GetItem(GetItemRequest) returns (Item) {
    option (google.api.http) = {get: "/v1/items/{id}"};
    option (pckt.openapi.operation) = {operation_id: "getItem"};
  }
}
```

| Extension                  | Applies to | Options                                                                                                    |
| -------------------------- | ---------- | ---------------------------------------------------------------------------------------------------------- |
| `(pckt.openapi.field)`     | fields     | `example`, `type`, `format`, `pattern`, `deprecated`, `hidden`, `required`, `read_only`, `write_only`, `minimum`, `maximum`, `min_length`, `max_length`, `min_items`, `max_items`, `extensions` |
| `(pckt.openapi.schema)`    | messages   | `title`, `type`, `hidden`, `example` (JSON), `deprecated`, `extensions`                                     |
| `(pckt.openapi.operation)` | methods    | `summary`, `tags`, `operation_id`, `deprecated`, `hidden`, `extensions`, `request_example`, `response_example`, `request_content_type`, `response_content_type` |
| `(pckt.openapi.tag)`       | services   | `name`, `external_docs`                                                                                    |

`extensions` keys must start with `x-`.

Request and response examples are set per method, inline or from a file
relative to the plugin `examples_dir` option, so big examples stay out of protos:

```proto
option (pckt.openapi.operation) = {
  response_example: {file: "shop/get_item.json"}
};
```

With `infer_schema: true` on an example, the body schema is inferred from it,
for untyped bodies such as `google.protobuf.Struct`; the example itself is not
written, each property gets a sample value instead:

```proto
option (pckt.openapi.operation) = {
  response_example: {file: "shop/stats.json" infer_schema: true}
};
```

`google.api.HttpBody` requests and responses are documented as raw bodies,
with the content type set by `request_content_type` / `response_content_type`.

## Go code

Go code for these annotations is published in the Go module
`github.com/pckt-sh/openapi`, package `github.com/pckt-sh/openapi/proto/pckt/openapi`.
When your Go code imports a file using the annotations, `protoc-gen-go` imports
that package. With buf managed mode, keep the `go_package` of this module:

```yaml
managed:
  enabled: true
  disable:
    - file_option: go_package
      module: buf.build/pckt/openapi
```

See the [repository](https://github.com/pckt-sh/openapi) for the plugin, the
merge tool and the full mapping rules.
