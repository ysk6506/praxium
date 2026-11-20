package ingest

// SchemeNumbers gives each identifier scheme of the model the number the received rows keep it by (stage_claim.scheme_no): its
// place in the order the model lists the schemes, from 1. The same definition set always gives the same numbers, so a step that
// reads the rows of a run by the definition set the run began with can turn a number back into the scheme; a run does not go on
// when its definition set changes. The caller makes the map once and keeps it.
func (p *Plan) SchemeNumbers() map[string]int {
	schemes := p.reg.IdentifierSchemes()
	out := make(map[string]int, len(schemes))
	for i, s := range schemes {
		out[s.ID.String()] = i + 1
	}
	return out
}
