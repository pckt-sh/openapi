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
// and surrounding blank lines.
func cleanComment(c string) string {
	lines := strings.Split(c, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(strings.TrimPrefix(line, " "), " \t")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// splitSummary splits a comment into its first line, used as summary, and
// the remaining text, used as description.
func splitSummary(c string) (string, string) {
	summary, rest, _ := strings.Cut(c, "\n")
	return strings.TrimSpace(summary), strings.TrimSpace(rest)
}
