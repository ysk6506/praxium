package pgschema

import (
	"crypto/sha256"
	"encoding/binary"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The digest of the input of an apply that works from ingest_diff (ADR-0095, the rule of ADR-0086 decision on the
// input digest). The digest is a hash over the rows of ingest_diff in their stored order, and each row is a line
// of fixed width, so the statement of a chunk can make the bytes in SQL and the bytes mean one thing in Go and
// in SQL. This file is the one place that says what the line is; DiffLineExpr is the same line as SQL, and the
// tests run both on the same rows.
//
//	line   = kind (1 byte) | type number (2 bytes) | entity number (8 bytes) | change (1 byte) | old hash (32) | new hash (32)
//	chunk  = SHA-256(line of the first row | line of the second row | ...), in the order of ord
//	chain  = SHA-256(DigestTag (16 bytes) | the chain before (32) | the chunk (32)); the chain of nothing is 32 zero bytes
//
// The numbers are big-endian. A hash that a row does not have (an object that is new has no old one) is 32
// zero bytes. The chunk is made by the statement of the chunk; the chain is made by Go, one chunk after another.
const (
	// DigestTag is the tag of this rule. It is 16 bytes, so the chain has a fixed width too.
	DigestTag = "praxium.batch.v2"
	// DiffLineSize is the width of a line in bytes.
	DiffLineSize = 1 + 2 + 8 + 1 + sha256.Size + sha256.Size
)

// KeyHashSigSize is the width of sig_hash, the hash of the shape of a key and of its values without the parent.
const KeyHashSigSize = 16

// DiffLine is a row of ingest_diff as the digest sees it.
type DiffLine struct {
	Kind     int16
	TypeNo   int16
	EntityNo int64
	Change   int16
	// OldHash and NewHash are nil for a hash the row does not have.
	OldHash, NewHash []byte
}

// Encode is the bytes of the line. It reports a field that does not fit its width or a hash that is not 32 bytes.
func (l DiffLine) Encode() ([]byte, error) {
	if l.Kind < 0 || l.Kind > 255 || l.Change < 0 || l.Change > 255 || l.TypeNo < 0 {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "a line of the difference has a kind %d, a type %d or a change %d out of its width", l.Kind, l.TypeNo, l.Change)
	}
	for _, h := range [][]byte{l.OldHash, l.NewHash} {
		if h != nil && len(h) != sha256.Size {
			return nil, kernel.Errorf(kernel.CodeInvalidValue, "a hash of a line of the difference is %d bytes, not %d", len(h), sha256.Size)
		}
	}
	out := make([]byte, 0, DiffLineSize)
	out = append(out, byte(l.Kind))
	out = binary.BigEndian.AppendUint16(out, uint16(l.TypeNo))
	out = binary.BigEndian.AppendUint64(out, uint64(l.EntityNo))
	out = append(out, byte(l.Change))
	for _, h := range [][]byte{l.OldHash, l.NewHash} {
		if h == nil {
			h = make([]byte, sha256.Size)
		}
		out = append(out, h...)
	}
	return out, nil
}

// ChunkDigest is the SHA-256 of the lines of a chunk, in the order given.
func ChunkDigest(lines []DiffLine) ([sha256.Size]byte, error) {
	h := sha256.New()
	for _, l := range lines {
		b, err := l.Encode()
		if err != nil {
			return [sha256.Size]byte{}, err
		}
		h.Write(b)
	}
	var out [sha256.Size]byte
	copy(out[:], h.Sum(nil))
	return out, nil
}

// ChainDigest puts a chunk after the chain of the chunks before it. The first chunk follows the zero chain.
func ChainDigest(before, chunk [sha256.Size]byte) [sha256.Size]byte {
	buf := make([]byte, 0, len(DigestTag)+2*sha256.Size)
	buf = append(buf, DigestTag...)
	buf = append(buf, before[:]...)
	buf = append(buf, chunk[:]...)
	return sha256.Sum256(buf)
}

// InputDigest is the digest of the whole input: the chunks, in order, each a list of lines.
func InputDigest(chunks [][]DiffLine) ([sha256.Size]byte, error) {
	var chain [sha256.Size]byte
	for _, c := range chunks {
		d, err := ChunkDigest(c)
		if err != nil {
			return chain, err
		}
		chain = ChainDigest(chain, d)
	}
	return chain, nil
}

// KeyRowHash is the new hash of a row of the kind key: what the key is, as a hash, so the digest covers the key
// that is bound and not only the object it is bound to. It is SHA-256(type number (2 bytes) | context number (8
// bytes) | sig_hash (16 bytes)); KeyRowHashExpr is the same in SQL.
func KeyRowHash(typeNo int16, ctxNo int64, sigHash []byte) ([sha256.Size]byte, error) {
	if len(sigHash) != KeyHashSigSize || typeNo < 0 {
		return [sha256.Size]byte{}, kernel.Errorf(kernel.CodeInvalidValue, "a key has a type %d and a sig_hash of %d bytes (it is %d)", typeNo, len(sigHash), KeyHashSigSize)
	}
	buf := binary.BigEndian.AppendUint16(nil, uint16(typeNo))
	buf = binary.BigEndian.AppendUint64(buf, uint64(ctxNo))
	return sha256.Sum256(append(buf, sigHash...)), nil
}

// DiffLineExpr is the line of a row of ingest_diff as an expression of SQL, for the row of the table that is
// named by alias (an empty alias is the table in the statement).
func DiffLineExpr(alias string) string {
	p := ""
	if alias != "" {
		p = QuoteIdent(alias) + "."
	}
	zero := "'\\x" + zeros64 + "'::bytea"
	return "substring(int2send(" + p + ColKind + ") from 2) || int2send(" + p + ColTypeNo + ") || int8send(" + p + ColEntityNo +
		") || substring(int2send(" + p + ColChangeKind + ") from 2) || COALESCE(" + p + ColOldHash + ", " + zero +
		") || COALESCE(" + p + ColNewHash + ", " + zero + ")"
}

// KeyRowHashExpr is KeyRowHash as an expression of SQL over the type number, the context number and the sig_hash
// of the row named by alias.
func KeyRowHashExpr(alias string) string {
	p := ""
	if alias != "" {
		p = QuoteIdent(alias) + "."
	}
	return "sha256(int2send(" + p + ColTypeNo + ") || int8send(" + p + ColCtxNo + ") || " + p + ColSigHash + ")"
}

const zeros64 = "0000000000000000000000000000000000000000000000000000000000000000"

// ChunkDigestSQL is the query that gives the digest of one chunk of ingest_diff of a KG, with the number of the
// chunk as its first parameter: the SHA-256 of the lines in the order of ord. A chunk that has no row gives NULL.
func ChunkDigestSQL(kgNo int64) string {
	return "SELECT sha256(string_agg(" + DiffLineExpr("") + ", ''::bytea ORDER BY " + ColOrd + ")) FROM " +
		TableRef(kgNo, TableIngestDiff) + " WHERE " + ColChunkNo + " = $1"
}
