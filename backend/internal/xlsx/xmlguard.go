package xlsx

import (
	"bytes"
	"fmt"
	"io"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The limits of the XML of every part (ADR-0103). They are fixed: no source definition changes them.
const (
	// maxXMLToken is the most bytes of one tag, one stretch of text, one comment, one CDATA section or one processing
	// instruction. encoding/xml gathers a comment or a CDATA section up to its end whatever its size, and a "<" may come inside
	// it, so the size of what lies between two "<" is no bound.
	maxXMLToken = 1 << 20
	// maxXMLDepth is how deep the elements of a part may be nested. encoding/xml keeps a frame for every open element.
	maxXMLDepth = 64
)

type guardState int

const (
	gsText guardState = iota
	gsOpen            // after "<"
	gsTag             // inside a start tag or an end tag
	gsBang            // after "<!": a comment or a CDATA section, or a declaration that is refused
	gsComment
	gsCData
	gsPI
)

// xmlGuard is a reader that watches the bytes of an XML part go by and stops the part when it holds what the decoder
// would gather without a bound: a token over maxXMLToken, elements nested over maxXMLDepth, a document type declaration, or a
// processing instruction other than the XML declaration. It does not parse: it only tells the kinds of token apart.
type xmlGuard struct {
	r       io.Reader
	part    string
	err     error
	state   guardState
	n       int  // bytes of the token being read
	depth   int  // elements open
	quote   byte // the quote an attribute value of a tag is in, or 0
	closing bool
	fresh   bool // the token just began its content, so the bytes before it count for nothing
	prev    byte // the last two bytes
	prev2   byte
	head    []byte // the first bytes after "<!" and "<?", to tell what they open
}

func newXMLGuard(part string, r io.Reader) *xmlGuard { return &xmlGuard{r: r, part: part} }

// Read reads from the part and checks what it read. The bytes of a part that fails are not given.
func (g *xmlGuard) Read(p []byte) (int, error) {
	if g.err != nil {
		return 0, g.err
	}
	n, err := g.r.Read(p)
	if n > 0 {
		if gerr := g.scan(p[:n]); gerr != nil {
			g.err = gerr
			return 0, gerr
		}
	}
	return n, err
}

func (g *xmlGuard) scan(b []byte) error {
	for i := 0; i < len(b); i++ {
		if g.state == gsText {
			// the common case: skip to the next "<" at once
			j := bytes.IndexByte(b[i:], '<')
			if j < 0 {
				g.n += len(b) - i
				return g.size("a stretch of text")
			}
			g.n += j
			if err := g.size("a stretch of text"); err != nil {
				return err
			}
			i += j
			g.state, g.n, g.prev, g.prev2 = gsOpen, 1, '<', 0
			continue
		}
		if err := g.step(b[i]); err != nil {
			return err
		}
	}
	return nil
}

func (g *xmlGuard) size(what string) error {
	if g.n > maxXMLToken {
		return g.damaged(fmt.Sprintf("%s is larger than %d bytes", what, maxXMLToken))
	}
	return nil
}

func (g *xmlGuard) damaged(why string) error {
	return kernel.Errorf(kernel.CodeInvalidValue, "the workbook is damaged: in the part %s, %s: this limit is fixed", g.part, why)
}

// step takes one byte that is not in plain text.
func (g *xmlGuard) step(c byte) error {
	g.n++
	err := g.inToken(c)
	if g.fresh {
		g.prev, g.prev2, g.fresh = 0, 0, false
	} else {
		g.prev2, g.prev = g.prev, c
	}
	return err
}

func (g *xmlGuard) inToken(c byte) error {
	switch g.state {
	case gsOpen:
		return g.open(c)
	case gsTag:
		return g.tag(c)
	case gsBang:
		return g.bang(c)
	case gsPI:
		return g.pi(c)
	case gsComment:
		return g.until(c, '-', "a comment")
	}
	return g.until(c, ']', "a CDATA section")
}

// until ends a comment or a CDATA section at the ">" that follows two of the mark, and bounds its size.
func (g *xmlGuard) until(c, mark byte, what string) error {
	if c == '>' && g.prev == mark && g.prev2 == mark {
		g.end()
		return nil
	}
	return g.size(what)
}

// end ends a token and goes back to plain text.
func (g *xmlGuard) end() { g.state, g.n, g.head, g.quote = gsText, 0, g.head[:0], 0 }

func (g *xmlGuard) open(c byte) error {
	g.head = g.head[:0]
	switch c {
	case '/':
		g.state, g.closing = gsTag, true
	case '?':
		g.state = gsPI
	case '!':
		g.state = gsBang
	default:
		g.state, g.closing = gsTag, false
	}
	return nil
}

func (g *xmlGuard) tag(c byte) error {
	switch {
	case g.quote != 0:
		if c == g.quote {
			g.quote = 0
		}
	case c == '"' || c == '\'':
		g.quote = c
	case c == '>':
		return g.closeTag()
	}
	return g.size("a tag")
}

// closeTag counts the elements a tag opens and closes.
func (g *xmlGuard) closeTag() error {
	switch {
	case g.closing:
		g.depth = max(0, g.depth-1)
	case g.prev != '/':
		g.depth++
		if g.depth > maxXMLDepth {
			return g.damaged(fmt.Sprintf("elements nested deeper than %d levels", maxXMLDepth))
		}
	}
	g.end()
	return nil
}

// bang tells a comment and a CDATA section from the declarations that are refused, from the bytes after "<!".
func (g *xmlGuard) bang(c byte) error {
	g.head = append(g.head, c)
	switch string(g.head) {
	case "-":
		return nil
	case "--":
		g.state, g.fresh = gsComment, true
		return nil
	case "[CDATA[":
		g.state, g.fresh = gsCData, true
		return nil
	}
	if bytes.HasPrefix([]byte("[CDATA["), g.head) {
		return nil
	}
	return g.damaged("a document type declaration, which this build does not read")
}

// pi checks a processing instruction: only the XML declaration is read.
func (g *xmlGuard) pi(c byte) error {
	if len(g.head) < 4 {
		g.head = append(g.head, c)
		if len(g.head) == 4 && !xmlDeclStart(g.head) {
			return g.damaged("a processing instruction, which this build does not read")
		}
	}
	if c == '>' && g.prev == '?' {
		if len(g.head) < 4 && !xmlDeclStart(append(g.head, ' ')) {
			return g.damaged("a processing instruction, which this build does not read")
		}
		g.end()
		return nil
	}
	return g.size("a processing instruction")
}

// xmlDeclStart tells whether the first bytes of a processing instruction are the target xml and a space or the end.
func xmlDeclStart(h []byte) bool {
	if len(h) < 4 || string(h[:3]) != "xml" {
		return false
	}
	return h[3] == ' ' || h[3] == '\t' || h[3] == '\r' || h[3] == '\n' || h[3] == '?'
}
