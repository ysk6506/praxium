package httpsource

import (
	"errors"
	"strings"
	"testing"
)

// N2: a body of JSON lines that the source compressed, although the request asked for identity, fails as not JSON, and the reason
// says that the source compressed it. A body that is not compressed keeps the reason as it was.
func TestCompressedNoteSaysTheSourceIgnoredIdentity(t *testing.T) {
	base := &formatError{"value 1 of the answer is not JSON"}
	got := compressedNote(base, map[string][]string{"Content-Encoding": {"gzip"}})
	if !strings.Contains(got.Error(), "Content-Encoding: gzip") || !strings.Contains(got.Error(), "although the request asked for identity") {
		t.Fatalf("the reason does not say the source compressed the answer: %q", got)
	}
	var fe *formatError
	if !errors.As(got, &fe) {
		t.Fatal("the failure is no longer a format failure")
	}
	if plain := compressedNote(base, map[string][]string{"Content-Encoding": {"identity"}}); plain != error(base) {
		t.Fatalf("an identity body changed the reason: %q", plain)
	}
	if plain := compressedNote(base, nil); plain != error(base) {
		t.Fatalf("a body without a coding changed the reason: %q", plain)
	}
}
