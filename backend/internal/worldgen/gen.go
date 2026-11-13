package worldgen

import (
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// gen is one run of the generator.
type gen struct {
	p     Params
	sch   *schema
	k     worldKeys
	lay   *Layout
	obs   []Observer
	err   error
	b     builder
	dates []string

	members []ref
	podSeq  int

	object Object
	lnk    Link
	// buffers behind the records handed to observers
	propBuf []propVal
	unkBuf  []int
	asmBuf  []int
	lpBuf   []propVal
}

func newGen(p Params, sch *schema, k worldKeys, lay *Layout, obs []Observer) *gen {
	return &gen{p: p, sch: sch, k: k, lay: lay, obs: obs, dates: dateTable()}
}

func (g *gen) fail(err error) {
	if g.err == nil {
		g.err = err
	}
}

func (g *gen) sendObject(o *Object) {
	for _, ob := range g.obs {
		if err := ob.Object(o); err != nil {
			g.fail(err)
			return
		}
	}
}

func (g *gen) sendLink(l *Link) {
	for _, ob := range g.obs {
		if err := ob.Link(l); err != nil {
			g.fail(err)
			return
		}
	}
}

func (g *gen) scope(kind Scope, site, index int, begin bool) {
	for _, ob := range g.obs {
		if err := ob.Scope(kind, site, index, begin); err != nil {
			g.fail(err)
			return
		}
	}
}

// newSchemaFor loads the standard model and builds the schema; the world
// depends on nothing else.
func newSchemaFor(reg *model.Registry) (*schema, error) {
	if reg == nil {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "a registry is required")
	}
	return newSchema(reg)
}
