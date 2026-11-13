package contract

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

func parts(files map[string]string) fstest.MapFS {
	fsys := fstest.MapFS{}
	for name, content := range files {
		fsys["parts/"+name] = &fstest.MapFile{Data: []byte(content)}
	}
	return fsys
}

// [REQ-143]
func TestAssembleBuildsOneCanonicalDocument(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{
			"merges objects by key and sorts keys",
			map[string]string{
				"10-b.json": `{"paths": {"/v1/b": {"get": {}}}}`,
				"00-a.json": `{"openapi": "3.1.0", "paths": {"/v1/a": {"get": {}}}}`,
			},
			"{\n  \"openapi\": \"3.1.0\",\n  \"paths\": {\n    \"/v1/a\": {\n      \"get\": {}\n    },\n    \"/v1/b\": {\n      \"get\": {}\n    }\n  }\n}\n",
		},
		{
			"does not escape angle brackets",
			map[string]string{"00.json": `{"openapi": "3.1.0", "info": {"description": "<a> & <b>"}}`},
			"{\n  \"info\": {\n    \"description\": \"<a> & <b>\"\n  },\n  \"openapi\": \"3.1.0\"\n}\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Assemble(parts(tt.files), "parts")
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Fatalf("got\n%s\nwant\n%s", got, tt.want)
			}
			again, _ := Assemble(parts(tt.files), "parts")
			if string(again) != string(got) {
				t.Fatal("the same parts gave different bytes")
			}
		})
	}
}

// [REQ-143]
func TestAssembleRefusesWhatIsAmbiguous(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"two owners of one path", map[string]string{
			"00.json": `{"openapi": "3.1.0", "paths": {"/v1/a": {"summary": "x"}}}`, "01.json": `{"paths": {"/v1/a": {"summary": "y"}}}`}, "summary"},
		{"no version", map[string]string{"00.json": `{"paths": {}}`}, "3.1.0"},
		{"wrong version", map[string]string{"00.json": `{"openapi": "3.0.3"}`}, "3.1.0"},
		{"not JSON", map[string]string{"00.json": `{`}, "00.json"},
		{"not an object", map[string]string{"00.json": `[]`}, "00.json"},
		{"empty folder", map[string]string{"readme.txt": `x`}, "no contract part"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Assemble(parts(tt.files), "parts")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tt.want)
			}
			if code := kernel.CodeOf(err); code != kernel.CodeInvalidValue && code != kernel.CodeConflict {
				t.Fatalf("code = %q", code)
			}
		})
	}
}

// [REQ-143]
func TestAssembleReportsAMissingFolder(t *testing.T) {
	if _, err := Assemble(fstest.MapFS{}, "nowhere"); err == nil {
		t.Fatal("want an error")
	}
}
