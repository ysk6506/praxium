package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// brokenStore is a store that cannot say whether a KG is there.
type brokenStore struct{ graph.Store }

func (brokenStore) GetKG(context.Context, string) (graph.KG, error) {
	return graph.KG{}, errors.New("the disk does not answer")
}

// [REQ-294]
func TestLocalStopsOnAStoreErrorAndDoesNotCallItAnEmptyFolder(t *testing.T) {
	err := requireKG(context.Background(), brokenStore{}, "data")
	if err == nil || !strings.Contains(err.Error(), "the disk does not answer") || kernel.CodeOf(err) == kernel.CodeNotFound {
		t.Fatalf("a store that fails is not a folder with no KG: %v", err)
	}
}

func TestADevelopmentServerSaysWhenItsTruthFileCannotBeRead(t *testing.T) {
	d, _, stop := apiDeps(map[string]string{"/dev-token": fakeToken + "\n"}, nil)
	defer stop()
	var out, errOut bytes.Buffer
	args := []string{"api", "serve", "-addr", "127.0.0.1:0", "-mode", "development", "-dev-token-file", "/dev-token", "-dev-truth", "/no/such/truth.jsonl"}
	if code := execute(args, &out, &errOut, d); code != exitFailure || !strings.Contains(errOut.String(), "cannot read the truth file") {
		t.Fatalf("a truth file that is not there must stop the start and say so: %d %s", code, errOut.String())
	}
}
