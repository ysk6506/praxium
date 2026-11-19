package ingest

import (
	"io"
	"unicode/utf8"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// guardPrefix is how many bytes at the start of a stream are looked at for a byte order mark and for the NUL bytes of
// UTF-16 text, the sample CheckEncoding looks at.
const guardPrefix = 2048

// encodingGuard is a reader that refuses a stream that is not UTF-8 text, as CheckEncoding refuses a file, without
// holding the stream: the start is checked with CheckEncoding itself, and the rest as it passes, a character that is
// cut between two reads being held back until its end comes. The bytes pass on unchanged. What it refused is in bad.
type encodingGuard struct {
	src     io.Reader
	parser  string
	head    []byte
	started bool
	// off is the offset of the first byte that is not yet checked; tail is the start of a character cut by the last read.
	off  int64
	tail []byte
	// failed is an error of the source that came while the sample was read; it is given after the sample's bytes.
	failed error
	bad    error
}

func newEncodingGuard(src io.Reader, parser string) *encodingGuard {
	return &encodingGuard{src: src, parser: parser}
}

func (g *encodingGuard) Read(p []byte) (int, error) {
	if g.bad != nil {
		return 0, g.bad
	}
	if !g.started {
		g.started = true
		if err := g.start(); err != nil {
			return 0, err
		}
	}
	if len(g.head) > 0 {
		n := copy(p, g.head)
		g.head = g.head[n:]
		return n, nil
	}
	if g.failed != nil {
		return 0, g.failed
	}
	n, err := g.src.Read(p)
	if n > 0 {
		if bad := g.check(p[:n], false); bad != nil {
			return 0, bad
		}
	}
	if err == io.EOF {
		if bad := g.check(nil, true); bad != nil {
			return 0, bad
		}
	}
	return n, err
}

// start reads the sample from the start and checks it as a file is checked.
func (g *encodingGuard) start() error {
	buf := make([]byte, guardPrefix)
	n, err := io.ReadFull(g.src, buf)
	g.head = buf[:n]
	short := err == io.EOF || err == io.ErrUnexpectedEOF
	if err != nil && !short {
		g.failed = err
	}
	cut := n
	if !short {
		cut -= incompleteTail(g.head)
	}
	if e := CheckEncoding(g.head[:cut], g.parser); e != nil {
		g.bad = e
		return e
	}
	g.off, g.tail = int64(cut), append([]byte(nil), g.head[cut:]...)
	if short {
		g.failed = io.EOF
	}
	return nil
}

// check validates the bytes that came after the checked ones, together with the cut character held back, and keeps a
// new cut character for the next read. At the end of the stream nothing can be held back.
func (g *encodingGuard) check(chunk []byte, end bool) error {
	buf := chunk
	if len(g.tail) > 0 {
		buf = append(append([]byte(nil), g.tail...), chunk...)
	}
	cut := len(buf)
	if !end {
		cut -= incompleteTail(buf)
	}
	if i := firstInvalidUTF8(buf[:cut]); i >= 0 {
		g.bad = g.invalid(g.off + int64(i))
		return g.bad
	}
	g.off += int64(cut)
	g.tail = append(g.tail[:0], buf[cut:]...)
	return nil
}

// invalid is the error of CheckEncoding for a byte that does not begin a valid character, at an offset of the stream.
func (g *encodingGuard) invalid(offset int64) error {
	return kernel.Errorf(kernel.CodeInvalidValue, "the file is not UTF-8 text (the byte at offset %d does not begin a valid character): it is probably in a local code page such as EUC-KR or CP949, which this build does not decode. %s", offset, encodingFix(g.parser))
}

// incompleteTail is the number of bytes at the end of buf that are the start of a character that is not all there yet.
func incompleteTail(buf []byte) int {
	for k := 1; k < utf8.UTFMax && k <= len(buf); k++ {
		b := buf[len(buf)-k]
		if utf8.RuneStart(b) {
			if b >= utf8.RuneSelf && !utf8.FullRune(buf[len(buf)-k:]) {
				return k
			}
			return 0
		}
	}
	return 0
}
