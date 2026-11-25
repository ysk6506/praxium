package ingest

import (
	"slices"
	"strings"
	"testing"
)

// N5: a natural key whose column reads the element first and the outer record after is a definition that is read (the first path
// decides which record a column reads). A column whose first path reads the outer record is refused, as it was.
func TestN5NaturalKeyColumnWithElementFirstIsAccepted(t *testing.T) {
	codes := hxdCodes(t, hxdFile(
		`{"id":"d","parser":"json_lines","records_path":"[*].ifs","natural_key":["nm"],"columns":[{"name":"nm","paths":["name","^.name"]}]}`))
	if len(codes) != 0 {
		t.Fatalf("the definition is refused: %q", codes)
	}
}

func TestN5NaturalKeyColumnWithOuterFirstIsRefused(t *testing.T) {
	codes := hxdCodes(t, hxdFile(
		`{"id":"d","parser":"json_lines","records_path":"[*].ifs","natural_key":["nm"],"columns":[{"name":"nm","paths":["^.name","name"]}]}`))
	if !slices.ContainsFunc(codes, func(c string) bool { return strings.HasPrefix(c, "source.dataset.expand_invalid ") }) {
		t.Fatalf("the definition is not refused: %q", codes)
	}
}

// N5: in the flow of the adapters, a file of JSON lines without [*] that reads ^. is a definition error, as HTTP's is.
func TestN5FileJSONLinesCaretWithoutAnArrayIsAPathError(t *testing.T) {
	codes := hxdCodes(t, hxdFile(
		`{"id":"d","parser":"json_lines","records_path":"ifs","natural_key":["id"],"columns":[{"name":"id","paths":["^.id"]}]}`))
	if !slices.ContainsFunc(codes, func(c string) bool { return strings.HasPrefix(c, "source.path.invalid ") }) {
		t.Fatalf("the definition is not refused as a path: %q", codes)
	}
}
