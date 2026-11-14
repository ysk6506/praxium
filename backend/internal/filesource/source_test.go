package filesource

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

type failing struct{}

func (failing) Read([]byte) (int, error) { return 0, errors.New("broken") }
func (failing) Close() error             { return nil }

func opener(files map[string]io.ReadCloser) Opener {
	return func(name string) (io.ReadCloser, error) {
		f, ok := files[name]
		if !ok {
			return nil, errors.New("no such file")
		}
		return f, nil
	}
}

func text(s string) io.ReadCloser { return io.NopCloser(strings.NewReader(s)) }

// [REQ-263]
func TestFetchReadsTheFile(t *testing.T) {
	s := Source{Open: opener(map[string]io.ReadCloser{"a.csv": text("x,y\n1,2\n")}), Files: map[string]string{"ds": "a.csv"}}
	c, err := s.Fetch(context.Background(), "src", ingest.Dataset{ID: "ds"})
	if err != nil || !c.Complete || string(c.Data) != "x,y\n1,2\n" || c.Hash == "" {
		t.Fatalf("%+v %v", c, err)
	}
}

// [REQ-263]
func TestDatasetWithoutFileIsNotCollected(t *testing.T) {
	_, err := Source{Files: map[string]string{}}.Fetch(context.Background(), "src", ingest.Dataset{ID: "ds"})
	if kernel.CodeOf(err) != kernel.CodeNotFound {
		t.Fatalf("got %v", err)
	}
}

// [REQ-263]
func TestFileThatCannotBeOpenedIsAnError(t *testing.T) {
	s := Source{Open: opener(nil), Files: map[string]string{"ds": "missing.csv"}}
	_, err := s.Fetch(context.Background(), "src", ingest.Dataset{ID: "ds"})
	if err == nil || kernel.CodeOf(err) == kernel.CodeNotFound {
		t.Fatalf("got %v", err)
	}
}

// [REQ-267]
func TestLargeOrBrokenFilesAreIncomplete(t *testing.T) {
	big := Source{Open: opener(map[string]io.ReadCloser{"a": text("0123456789")}), Files: map[string]string{"ds": "a"}, MaxBytes: 5}
	c, err := big.Fetch(context.Background(), "src", ingest.Dataset{ID: "ds"})
	if err != nil || c.Complete || c.Reason == "" || len(c.Data) != 0 {
		t.Fatalf("over the limit: %+v %v", c, err)
	}
	broken := Source{Open: opener(map[string]io.ReadCloser{"a": failing{}}), Files: map[string]string{"ds": "a"}}
	c, err = broken.Fetch(context.Background(), "src", ingest.Dataset{ID: "ds"})
	if err != nil || c.Complete {
		t.Fatalf("broken read: %+v %v", c, err)
	}
}
