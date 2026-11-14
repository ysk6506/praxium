package filejournal

import (
	"bufio"
	"io"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// segScan is what the reading of one log segment found.
type segScan struct {
	// header is the state of the header; only headerOK segments have records.
	header headerState
	// first is the number of the first record, from the header.
	first uint64
	// next is the number of the record that follows the last good one.
	next uint64
	// torn is true when the file goes on after the last good record with bytes that are not a whole record.
	torn bool
	// bytes is the size of the good records that were handed to apply (the ones past the snapshot).
	bytes int64
	// dropped is the size of the bytes after the last good record that were left out (zero unless torn).
	dropped int64
}

// scanner holds the state of the reading of one segment.
type scanner struct {
	name    string
	br      *bufio.Reader
	skip    uint64
	apply   func([]byte) error
	res     segScan
	payload []byte
}

// recordState is what the reading of one record found.
type recordState int

const (
	recordGood recordState = iota
	recordEnd
	recordBroken
)

// scanSegment reads the segment called name. A record with a number above skip goes to apply; it must not keep the
// payload, which is reused. A broken record at the end, with nothing whole after it, is a torn end (the crash cut
// the write that was in flight, which nobody was told had worked); a broken record with whole records after it is
// damage, and so is a whole record with the wrong number: both stop the reading with CodeCorrupt.
func scanSegment(name string, rc io.Reader, nameFirst, skip uint64, apply func([]byte) error) (segScan, error) {
	sc := &scanner{name: name, br: bufio.NewReaderSize(rc, 1<<20), skip: skip, apply: apply}
	hdr := make([]byte, headerLen)
	n, err := io.ReadFull(sc.br, hdr)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return segScan{}, kernel.Wrap(CodeIO, err, "cannot read "+name)
	}
	first, state, herr := parseHeader(hdr[:n], segMagic, name)
	if herr != nil {
		return segScan{}, herr
	}
	sc.res = segScan{header: state, first: first, next: nameFirst}
	if state != headerOK {
		// A header that is cut short or garbled: whatever follows may still hold whole records, which is damage.
		rest, _ := io.ReadAll(sc.br)
		if validFrameAfter(append(hdr[:n:n], rest...), 0) {
			return segScan{}, kernel.Errorf(CodeCorrupt, "%s has a damaged header but whole records after it", name)
		}
		sc.res.torn = true
		sc.res.dropped = int64(n + len(rest))
		return sc.res, nil
	}
	if first != nameFirst {
		return segScan{}, kernel.Errorf(CodeCorrupt, "%s starts at record %d, its name says %d", name, first, nameFirst)
	}
	sc.res.next = first
	return sc.records()
}

// records reads records until the end of the file or the first broken one.
func (sc *scanner) records() (segScan, error) {
	for {
		state, tail, err := sc.record()
		if err != nil {
			return segScan{}, err
		}
		switch state {
		case recordGood:
			continue
		case recordEnd:
			return sc.res, nil
		}
		return sc.broken(tail)
	}
}

// broken decides what a broken record is: a torn end, or damage.
func (sc *scanner) broken(tail []byte) (segScan, error) {
	rest, err := io.ReadAll(sc.br)
	if err != nil {
		return segScan{}, kernel.Wrap(CodeIO, err, "cannot read "+sc.name)
	}
	if validFrameAfter(append(tail, rest...), sc.res.next) {
		return segScan{}, kernel.Errorf(CodeCorrupt, "record %d of %s is damaged, and whole records follow it: this is not a cut write", sc.res.next, sc.name)
	}
	sc.res.torn = true
	sc.res.dropped = int64(len(tail) + len(rest))
	return sc.res, nil
}

// record reads one record. For a broken one it returns the bytes read so far.
func (sc *scanner) record() (recordState, []byte, error) {
	var h [frameLen]byte
	hn, err := io.ReadFull(sc.br, h[:])
	if err == io.EOF {
		return recordEnd, nil, nil
	}
	if err != nil && err != io.ErrUnexpectedEOF {
		return 0, nil, kernel.Wrap(CodeIO, err, "cannot read "+sc.name)
	}
	tail := append([]byte(nil), h[:hn]...)
	if hn < frameLen {
		return recordBroken, tail, nil
	}
	length, sum, recno := frameHeader(h[:])
	if length > maxRecord {
		return recordBroken, tail, nil
	}
	sc.payload = grow(sc.payload, int(length))
	pn, err := io.ReadFull(sc.br, sc.payload)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return 0, nil, kernel.Wrap(CodeIO, err, "cannot read "+sc.name)
	}
	if pn != int(length) || frameSum(h[:], sc.payload) != sum {
		return recordBroken, append(tail, sc.payload[:pn]...), nil
	}
	return recordGood, nil, sc.take(recno, int64(length))
}

// take accepts a whole record: it must be the expected one.
func (sc *scanner) take(recno uint64, length int64) error {
	if recno != sc.res.next {
		return kernel.Errorf(CodeCorrupt, "%s holds record %d where record %d belongs", sc.name, recno, sc.res.next)
	}
	if recno > sc.skip {
		if err := sc.apply(sc.payload); err != nil {
			return err
		}
		sc.res.bytes += frameLen + length
	}
	sc.res.next++
	return nil
}

func grow(b []byte, n int) []byte {
	if cap(b) < n {
		return make([]byte, n, max(n, 2*cap(b)))
	}
	return b[:n]
}
