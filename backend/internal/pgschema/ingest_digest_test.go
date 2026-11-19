package pgschema_test

import (
	"crypto/sha256"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The hash of an identifier row and the query of the digests of the chunks that are done (ADR-0099). The names of
// the tests start with Sap, so they never meet a name of another test file of this folder. That SQL gives the
// values these functions give is checked on a server, in adapter.pgstore.

// The hash of an identifier row is the SHA-256 of the three texts, each with its length in bytes in front, so that
// where one text ends and the next begins is part of the hash.
//
// [REQ-726]
func TestSapIdentRowHashHasTheLengthOfEachTextInFront(t *testing.T) {
	got := pgschema.IdentRowHash("ab", "c", "")
	want := sha256.Sum256([]byte("\x00\x00\x00\x02ab\x00\x00\x00\x01c\x00\x00\x00\x00"))
	if got != want {
		t.Fatalf("hash %x, wanted %x", got, want)
	}
	if pgschema.IdentRowHash("a", "bc", "") == got {
		t.Error("the boundary between the scheme and the value is not in the hash")
	}
	if pgschema.IdentRowHash("ab", "", "c") == got {
		t.Error("the boundary between the value and the scope is not in the hash")
	}
	if pgschema.IdentRowHash("서버", "é", "") == pgschema.IdentRowHash("서버", "e", "") {
		t.Error("a text that is not ASCII is not in the hash")
	}
}

// The expression of SQL spells the three columns of the row it is made for, with the prefix of its alias or none.
//
// [REQ-726]
func TestSapIdentRowHashExpressionNamesTheThreeColumns(t *testing.T) {
	expr := pgschema.IdentRowHashExpr("oi")
	for _, col := range []string{`"oi".scheme`, `"oi".value`, `"oi".scope`} {
		if strings.Count(expr, col) != 2 {
			t.Errorf("%s is not used twice (its length and its bytes) in %s", col, expr)
		}
	}
	if !strings.HasPrefix(expr, "sha256(") || strings.Contains(pgschema.IdentRowHashExpr(""), `".`) {
		t.Errorf("the expression: %s", expr)
	}
}

// The query of the digests of the chunks before a chunk reads one chunk at a time in the order of the chunks, from the
// table of the KG it is made for.
//
// [REQ-726]
func TestSapChunkDigestsQueryGroupsTheChunksBeforeTheOneItIsGiven(t *testing.T) {
	q := pgschema.ChunkDigestsSQL(7)
	for _, want := range []string{`FROM "s_7"."ingest_diff"`, "WHERE chunk_no < $1", "GROUP BY chunk_no", "ORDER BY chunk_no", "string_agg(", "ORDER BY ord)"} {
		if !strings.Contains(q, want) {
			t.Errorf("%q is not in %s", want, q)
		}
	}
}
