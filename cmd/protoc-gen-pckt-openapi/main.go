// Command protoc-gen-pckt-openapi generates one OpenAPI 3.1 document per .proto file.
package main

import (
	"fmt"
	"io"
	"os"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/pluginpb"

	"github.com/pckt-sh/openapi/internal/gen"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "protoc-gen-pckt-openapi:", err)
		os.Exit(1)
	}
}

func run() error {
	in, err := io.ReadAll(os.Stdin)
	if err != nil {
		return err
	}

	req := &pluginpb.CodeGeneratorRequest{}
	if err := proto.Unmarshal(in, req); err != nil {
		return fmt.Errorf("parse request: %w", err)
	}

	resp, err := gen.Run(req)
	if err != nil {
		resp = &pluginpb.CodeGeneratorResponse{Error: proto.String(err.Error())}
	}

	out, err := proto.Marshal(resp)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(out)
	return err
}
