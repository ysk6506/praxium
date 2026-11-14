package filejournal

import (
	"bufio"
	"encoding/binary"
	"hash"
	"hash/crc32"
	"io"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// snapWriter writes the body of a snapshot as chunks and, on finish, the end chunk and the checksum of the file.
type snapWriter struct {
	w   io.Writer
	crc hash.Hash32
	buf []byte
	err error
}

func newSnapWriter(w io.Writer, seq uint64) (*snapWriter, error) {
	s := &snapWriter{w: w, crc: crc32.New(crcTable), buf: make([]byte, 0, chunkSize)}
	s.put(putHeader(snapMagic, seq))
	return s, s.err
}

func (s *snapWriter) put(b []byte) {
	if s.err != nil {
		return
	}
	s.crc.Write(b)
	_, s.err = s.w.Write(b)
}

func (s *snapWriter) flush() {
	if len(s.buf) == 0 {
		return
	}
	var l [4]byte
	binary.BigEndian.PutUint32(l[:], uint32(len(s.buf)))
	s.put(l[:])
	s.put(s.buf)
	s.buf = s.buf[:0]
}

// Write implements io.Writer.
func (s *snapWriter) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 && s.err == nil {
		room := chunkSize - len(s.buf)
		take := min(room, len(p))
		s.buf = append(s.buf, p[:take]...)
		p = p[take:]
		if len(s.buf) == chunkSize {
			s.flush()
		}
	}
	return n - len(p), s.err
}

// finish writes the last chunk, the end chunk and the checksum.
func (s *snapWriter) finish() error {
	s.flush()
	s.put([]byte{0, 0, 0, 0})
	if s.err != nil {
		return s.err
	}
	var sum [4]byte
	binary.BigEndian.PutUint32(sum[:], s.crc.Sum32())
	_, s.err = s.w.Write(sum[:])
	return s.err
}

// snapReader reads the body of a snapshot back and checks the checksum of the whole file when the body ends. A
// snapshot that is cut short or damaged ends in CodeCorrupt, never in a quiet end of the body.
type snapReader struct {
	name string
	r    *bufio.Reader
	crc  hash.Hash32
	cur  []byte
	done bool
	seq  uint64
}

// openSnapshotStream reads the header of a snapshot file and returns the reader of its body.
func openSnapshotStream(name string, rc io.Reader) (*snapReader, error) {
	s := &snapReader{name: name, r: bufio.NewReaderSize(rc, 1<<20), crc: crc32.New(crcTable)}
	hdr := make([]byte, headerLen)
	n, err := io.ReadFull(s.r, hdr)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, kernel.Wrap(CodeIO, err, "cannot read "+name)
	}
	seq, state, herr := parseHeader(hdr[:n], snapMagic, name)
	if herr != nil {
		return nil, herr
	}
	if state != headerOK {
		return nil, kernel.Errorf(CodeCorrupt, "the header of %s is damaged or cut short", name)
	}
	s.crc.Write(hdr)
	s.seq = seq
	return s, nil
}

func (s *snapReader) corrupt(why string) error {
	return kernel.Errorf(CodeCorrupt, "snapshot %s %s", s.name, why)
}

// readFull fills p from the file and counts it in the checksum.
func (s *snapReader) readFull(p []byte) error {
	if _, err := io.ReadFull(s.r, p); err != nil {
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return s.corrupt("is cut short")
		}
		return kernel.Wrap(CodeIO, err, "cannot read "+s.name)
	}
	return nil
}

// Read implements io.Reader.
func (s *snapReader) Read(p []byte) (int, error) {
	for len(s.cur) == 0 {
		if s.done {
			return 0, io.EOF
		}
		var l [4]byte
		if err := s.readFull(l[:]); err != nil {
			return 0, err
		}
		s.crc.Write(l[:])
		length := binary.BigEndian.Uint32(l[:])
		if length == 0 {
			return 0, s.finish()
		}
		if length > maxChunk {
			return 0, s.corrupt("has a chunk longer than any the writer makes")
		}
		s.cur = make([]byte, length)
		if err := s.readFull(s.cur); err != nil {
			s.cur = nil
			return 0, err
		}
		s.crc.Write(s.cur)
	}
	n := copy(p, s.cur)
	s.cur = s.cur[n:]
	return n, nil
}

// finish checks the checksum and that nothing follows it.
func (s *snapReader) finish() error {
	var sum [4]byte
	if err := s.readFull(sum[:]); err != nil {
		return err
	}
	if binary.BigEndian.Uint32(sum[:]) != s.crc.Sum32() {
		return s.corrupt("does not match its checksum")
	}
	if _, err := s.r.ReadByte(); err != io.EOF {
		return s.corrupt("has bytes after its checksum")
	}
	s.done = true
	return io.EOF
}
