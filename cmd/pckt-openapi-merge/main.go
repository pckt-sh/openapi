// Command pckt-openapi-merge merges the OpenAPI documents generated per .proto
// file by protoc-gen-pckt-openapi into a single document.
//
//	pckt-openapi-merge -o openapi.yaml [-title T] [-version V] [-server URL]... [-base base.yaml] <files or dirs...>
//
// Directories are walked for *.openapi.yaml, *.openapi.yml and *.openapi.json
// files. The output format follows the -o extension (.json or YAML), `-`
// writes YAML to stdout.
package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/pckt-sh/openapi/internal/merge"
)

type listFlag []string

func (l *listFlag) String() string     { return strings.Join(*l, ",") }
func (l *listFlag) Set(v string) error { *l = append(*l, v); return nil }

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "pckt-openapi-merge:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		output  = flag.String("o", "-", "output file, .json for JSON, YAML otherwise; - for stdout")
		base    = flag.String("base", "", "base document providing info, servers, security...")
		opts    merge.Options
		servers listFlag
	)
	flag.StringVar(&opts.Title, "title", "", "info.title of the merged document")
	flag.StringVar(&opts.Version, "version", "", "info.version of the merged document")
	flag.StringVar(&opts.Description, "description", "", "info.description of the merged document")
	flag.Var(&servers, "server", "server URL, can be repeated")
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "usage: pckt-openapi-merge [flags] <files or dirs...>")
		flag.PrintDefaults()
	}
	flag.Parse()
	opts.Servers = servers
	opts.Warnings = os.Stderr

	if flag.NArg() == 0 {
		flag.Usage()
		return fmt.Errorf("no input")
	}

	files, err := collect(flag.Args(), *output)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no OpenAPI document found")
	}

	inputs := make([]merge.Input, 0, len(files))
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		inputs = append(inputs, merge.Input{Name: f, Data: data})
	}

	if *base != "" {
		data, err := os.ReadFile(*base)
		if err != nil {
			return err
		}
		opts.Base = &merge.Input{Name: *base, Data: data}
	}

	doc, err := merge.Merge(inputs, opts)
	if err != nil {
		return err
	}

	var out []byte
	if strings.EqualFold(filepath.Ext(*output), ".json") {
		out, err = merge.EncodeJSON(doc)
	} else {
		out, err = merge.EncodeYAML(doc)
	}
	if err != nil {
		return err
	}

	if *output == "-" {
		_, err = os.Stdout.Write(out)
		return err
	}
	if err := os.MkdirAll(filepath.Dir(*output), 0o755); err != nil {
		return err
	}
	return os.WriteFile(*output, out, 0o644)
}

// collect expands directories to the generated documents they contain, and
// returns a sorted, de-duplicated list excluding the output file.
func collect(args []string, output string) ([]string, error) {
	outAbs, _ := filepath.Abs(output)

	var files []string
	add := func(path string) {
		if abs, _ := filepath.Abs(path); abs != outAbs {
			files = append(files, filepath.Clean(path))
		}
	}

	for _, arg := range args {
		info, err := os.Stat(arg)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			add(arg)
			continue
		}
		err = filepath.WalkDir(arg, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && isGenerated(d.Name()) {
				add(path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	slices.Sort(files)
	return slices.Compact(files), nil
}

func isGenerated(name string) bool {
	for _, ext := range []string{".openapi.yaml", ".openapi.yml", ".openapi.json"} {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	return false
}
