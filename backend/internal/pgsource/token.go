package pgsource

import (
	"encoding/json"
	"slices"
)

// token is what a page carries so that a read can go on after it: the keys of the last row (the texts the server printed,
// which the server reads back as the argument of the next page), and the fingerprint of the dataset they belong to.
// It holds no secret, no host and no statement, and it is read as untrusted: it came back from the store of the
// state, so its keys are only ever sent as arguments of a prepared statement, never put into one.
type token struct {
	V  int      `json:"v"`
	FP string   `json:"fp"`
	K  []string `json:"k"`
}

const (
	tokenVersion = 1
	// maxTokenBytes is the most a token may have. The key of a row that makes a larger one is not carried: the page has no
	// token, and a read that would go on from it starts from the beginning.
	maxTokenBytes = 8 << 10
	// maxKeyBytes is the most one key text of a token may have when it is read back.
	maxKeyBytes = 4 << 10
)

func encodeToken(fp string, keys []string) []byte {
	out, err := json.Marshal(token{V: tokenVersion, FP: fp, K: keys})
	if err != nil || len(out) > maxTokenBytes {
		return nil
	}
	return out
}

// decodeToken reads the token of a page back. A token that is not this adapter's, not of this dataset or not of the
// shape of its key is no token: the read starts from the beginning.
func decodeToken(raw []byte, fp string, keys int) ([]string, bool) {
	if len(raw) == 0 || len(raw) > maxTokenBytes {
		return nil, false
	}
	var t token
	if err := json.Unmarshal(raw, &t); err != nil || t.V != tokenVersion || t.FP != fp || len(t.K) != keys {
		return nil, false
	}
	if slices.ContainsFunc(t.K, func(k string) bool { return len(k) > maxKeyBytes }) {
		return nil, false
	}
	return t.K, true
}
