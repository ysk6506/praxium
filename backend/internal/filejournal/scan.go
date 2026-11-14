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
}

// scanSegment reads the segment called name. A record with a number above skip goes to apply; it must not keep the
// payload, which is reused. A broken record at the end, with nothing whole after it, is a torn end (the crash cut
// the write that was in flight, which nobody was told had worked); a broken record with whole records after it is
// damage, and so is a whole record with the wrong number: both stop the reading with CodeCorrupt.
func scanSegment(name string, rc io.Reader, nameFirst, skip uint64, apply func([]byte) error) (segScan, error) {
	br := bufio.NewReaderSize(rc, 1<<20)
	hdr := make([]byte, headerLen)
	n, err := io.ReadFull(br, hdr)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return segScan{}, kernel.Wrap(CodeIO, err, "cannot read "+name)
	}
	first, state, herr := parseHeader(hdr[:n], segMagic, name)
	if herr != nil {
		return segScan{}, herr
	}
	res := segScan{header: state, first: first, next: nameFirst}
	if state != headerOK {
		// A header that is cut short or garbled: whatever follows may still hold whole records, which is damage.
		rest, _ := io.ReadAll(br)
		if validFrameAfter(append(hdr[:n:n], rest...), 0) {
			return segScan{}, kernel.Errorf(CodeCorrupt, "%s has a damaged header but whole records after it", name)
		}
		res.torn = true
		return res, nil
	}
	if first != nameFirst {
		return segScan{}, kernel.Errorf(CodeCorrupt, "%s starts at record %d, its name says %d", name, first, nameFirst)
	}
	res.next = first
	var payload []byte
	for {
		var h [frameLen]byte
		hn, err := io.ReadFull(br, h[:])
		if err == io.EOF {
			return res, nil
		}
		if err != nil && err != io.ErrUnexpectedEOF {
			return segScan{}, kernel.Wrap(CodeIO, err, "cannot read "+name)
		}
		tail := append([]byte(nil), h[:hn]...)
		if hn == frameLen {
			length, sum, recno := frameHeader(h[:])
			if length <= maxRecord {
				payload = grow(payload, int(length))
				pn, err := io.ReadFull(br, payload)
				if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
					return segScan{}, kernel.Wrap(CodeIO, err, "cannot read "+name)
				}
				if pn == int(length) && frameSum(h[:], payload) == sum {
					if recno != res.next {
						return segScan{}, kernel.Errorf(CodeCorrupt, "%s holds record %d where record %d belongs", name, recno, res.next)
					}
					if recno > skip {
						if err := apply(payload); err != nil {
							return segScan{}, err
						}
						res.bytes += frameLen + int64(length)
					}
					res.next++
					continue
				}
				tail = append(tail, payload[:pn]...)
			}
		}
		rest, err := io.ReadAll(br)
		if err != nil {
			return segScan{}, kernel.Wrap(CodeIO, err, "cannot read "+name)
		}
		if validFrameAfter(append(tail, rest...), res.next) {
			return segScan{}, kernel.Errorf(CodeCorrupt, "record %d of %s is damaged, and whole records follow it: this is not a cut write", res.next, name)
		}
		res.torn = true
		return res, nil
	}
}

func grow(b []byte, n int) []byte {
	if cap(b) < n {
		return make([]byte, n, max(n, 2*cap(b)))
	}
	return b[:n]
}
