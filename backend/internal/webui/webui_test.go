package webui_test

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ysk6506/praxium/backend/internal/webui"
)

// [REQ-275]
func TestPlaceholderIsAlwaysEmbedded(t *testing.T) {
	data, err := fs.ReadFile(webui.Files(), webui.Placeholder)
	if err != nil {
		t.Fatalf("the placeholder must be embedded: %v", err)
	}
	if !strings.Contains(string(data), "웹 UI가 포함되지 않은 빌드") {
		t.Fatalf("the placeholder must say that the build has no web UI, got %q", data)
	}
}

// [REQ-275]
func TestBuiltTellsTheBuiltUIFromThePlaceholder(t *testing.T) {
	if webui.Built(fstest.MapFS{webui.Placeholder: {Data: []byte("x")}}) {
		t.Fatal("a folder with only the placeholder is not a built UI")
	}
	if !webui.Built(fstest.MapFS{webui.Index: {Data: []byte("<html>")}}) {
		t.Fatal("a folder with index.html is a built UI")
	}
	if webui.Built(fstest.MapFS{webui.Index + "/x": {Data: []byte("x")}}) {
		t.Fatal("a folder named index.html is not a built UI")
	}
}
