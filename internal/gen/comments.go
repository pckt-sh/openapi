package gen

import (
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"
)

// comments returns the cleaned leading comments of a descriptor.
func comments(d protoreflect.Descriptor) string {
	loc := d.ParentFile().SourceLocations().ByDescriptor(d)
	return cleanComment(loc.LeadingComments)
}

// cleanComment strips the single space protoc keeps after `//` on every line,
// `buf:lint:ignore` directives and surrounding blank lines.
func cleanComment(c string) string {
	var lines []string
	for line := range strings.SplitSeq(c, "\n") {
		line = strings.TrimRight(strings.TrimPrefix(line, " "), " \t")
		if strings.HasPrefix(line, "buf:lint:ignore ") {
			continue
		}
		lines = append(lines, line)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
