package filejournal

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"strconv"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The format (ADR-0034). Numbers are big-endian.
//
// A file starts with a header of 20 bytes, whose layout every version keeps, so that a version that reads it first checks
// the checksum (a damaged header is damage) and then the version (a good header of another version is a file of
// another version of the product): the format name (6 bytes), the format version (2), the number of the
// first record of a log segment or the last record a snapshot holds (8) and the CRC-32C of those 16 bytes (4).
// A log segment goes on with frames: payload length (4), CRC-32C of the length, the record number and the payload
// (4), record number (8), payload. A snapshot goes on with chunks (length 4, bytes), an empty chunk to end, and the
// CRC-32C of everything before it (4).
const (
	segMagic  = "PRXLOG"
	snapMagic = "PRXSNP"
	// FormatVersion is the one version of the file format this build writes and reads.
	FormatVersion = 1

	headerLen = 20
	frameLen  = 16
	// maxRecord is the largest payload of a record. A length above it is damage, not a big record.
	maxRecord = 1 << 30
	// maxChunk is the largest chunk of a snapshot a reader accepts; the writer makes chunks of chunkSize.
	maxChunk  = 64 << 20
	chunkSize = 1 << 20
)

var crcTable = crc32.MakeTable(crc32.Castagnoli)

func segName(first uint64) string { return fmt.Sprintf("log-%020d.log", first) }
func snapName(seq uint64) string  { return fmt.Sprintf("snap-%020d.snap", seq) }
func tmpName(seq uint64) string   { return fmt.Sprintf("snap-%020d.tmp", seq) }

// parseName reads the number out of the name of a segment ("log"), a snapshot ("snap") or a temporary file ("tmp").
func parseName(name string) (kind string, n uint64, ok bool) {
	for _, f := range [...]struct{ prefix, suffix, kind string }{
		{"log-", ".log", "log"}, {"snap-", ".snap", "snap"}, {"snap-", ".tmp", "tmp"},
	} {
		if !strings.HasPrefix(name, f.prefix) || !strings.HasSuffix(name, f.suffix) {
			continue
		}
		digits := name[len(f.prefix) : len(name)-len(f.suffix)]
		if len(digits) != 20 {
			return "", 0, false
		}
		v, err := strconv.ParseUint(digits, 10, 64)
		return f.kind, v, err == nil
	}
	return "", 0, false
}

func putHeader(magic string, seq uint64) []byte {
	b := make([]byte, headerLen)
	copy(b, magic)
	binary.BigEndian.PutUint16(b[6:], FormatVersion)
	binary.BigEndian.PutUint64(b[8:], seq)
	binary.BigEndian.PutUint32(b[16:], crc32.Checksum(b[:16], crcTable))
	return b
}

// headerState says what a header is: a whole good one, one that is cut short (a crash while it was written), or
// damaged. A name or version that is not this build's is an error of its own, never a damage.
type headerState int

const (
	headerOK headerState = iota
	headerShort
	headerDamaged
)

// parseHeader reads a header of the file called name.
func parseHeader(b []byte, magic, name string) (uint64, headerState, error) {
	if len(b) < headerLen {
		if n := min(len(b), len(magic)); string(b[:n]) != magic[:n] {
			return 0, headerDamaged, nil
		}
		return 0, headerShort, nil
	}
	if string(b[:6]) != magic {
		return 0, headerDamaged, nil
	}
	if crc32.Checksum(b[:16], crcTable) != binary.BigEndian.Uint32(b[16:]) {
		return 0, headerDamaged, nil
	}
	if v := binary.BigEndian.Uint16(b[6:]); v != FormatVersion {
		return 0, headerDamaged, kernel.Errorf(CodeUnsupportedFormat,
			"%s has format version %d; this build reads version %d only (it was written by another version of the product: use that version to open this data folder)",
			name, v, FormatVersion)
	}
	return binary.BigEndian.Uint64(b[8:]), headerOK, nil
}

// appendFrame appends the frame of record number recno to dst.
func appendFrame(dst []byte, recno uint64, payload []byte) []byte {
	var h [frameLen]byte
	binary.BigEndian.PutUint32(h[0:], uint32(len(payload)))
	binary.BigEndian.PutUint64(h[8:], recno)
	c := crc32.Update(0, crcTable, h[0:4])
	c = crc32.Update(c, crcTable, h[8:16])
	c = crc32.Update(c, crcTable, payload)
	binary.BigEndian.PutUint32(h[4:], c)
	dst = append(dst, h[:]...)
	return append(dst, payload...)
}

// frameHeader splits the first 16 bytes of a frame.
func frameHeader(h []byte) (length uint32, sum uint32, recno uint64) {
	return binary.BigEndian.Uint32(h[0:]), binary.BigEndian.Uint32(h[4:]), binary.BigEndian.Uint64(h[8:])
}

// frameSum is the checksum a frame with this header and payload must carry.
func frameSum(h, payload []byte) uint32 {
	c := crc32.Update(0, crcTable, h[0:4])
	c = crc32.Update(c, crcTable, h[8:16])
	return crc32.Update(c, crcTable, payload)
}

// validFrameAfter reports whether b holds, at some offset from 1 on, a whole frame whose checksum is right and whose
// record number is at least min. It tells damage in the middle of a log from a torn end: nothing intact follows a
// torn write, while damage inside the log leaves the records after it whole.
func validFrameAfter(b []byte, min uint64) bool {
	for q := 1; q+frameLen <= len(b); q++ {
		length, sum, recno := frameHeader(b[q:])
		if recno < min || length > maxRecord || uint64(q)+frameLen+uint64(length) > uint64(len(b)) {
			continue
		}
		if frameSum(b[q:q+frameLen], b[q+frameLen:q+frameLen+int(length)]) == sum {
			return true
		}
	}
	return false
}
