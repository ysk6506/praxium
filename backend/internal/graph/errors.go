package graph

import (
	"sort"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The error codes of this unit. A code is never renamed or reused.
const (
	// CodeBatchInvalid is a change batch that breaks a rule of the registry or of its own shape.
	CodeBatchInvalid kernel.Code = "graph_batch_invalid"
	// CodeBindingMissing is a link between two types that no binding of its link type allows.
	CodeBindingMissing kernel.Code = "graph_binding_missing"
	// CodeCardinality is a link that breaks the cardinality of its binding.
	CodeCardinality kernel.Code = "graph_cardinality"
	// CodeDanglingLink is a link whose end is not live, or an object retired while a link still ends at it.
	CodeDanglingLink kernel.Code = "graph_dangling_link"
	// CodeVolatileInRow is a volatile attribute inside a versioned row.
	CodeVolatileInRow kernel.Code = "graph_volatile_in_row"
	// CodeVolatileOmitted is a volatile value asked from a past state: past reads omit volatile values.
	CodeVolatileOmitted kernel.Code = "graph_volatile_omitted"
	// CodeIdentifierTaken is an identifier that already names another object in the same scope.
	CodeIdentifierTaken kernel.Code = "graph_identifier_taken"
	// CodeHeadMoved is a write whose expected head is not the current head.
	CodeHeadMoved kernel.Code = "graph_head_moved"
	// CodeLeaseHeld is a lease request while another owner holds an unexpired lease.
	CodeLeaseHeld kernel.Code = "graph_lease_held"
	// CodeLeaseLost is a write by an owner whose lease expired or was taken over.
	CodeLeaseLost kernel.Code = "graph_lease_lost"
	// CodeSemanticTooLow is a lease request by an engine older than the one that applied last.
	CodeSemanticTooLow kernel.Code = "graph_semantic_too_low"
	// CodeApplyInProgress is a batch started while another apply of the KG is half done.
	CodeApplyInProgress kernel.Code = "graph_apply_in_progress"
	// CodeGenerationChanged is a read or write that names an older generation of the KG.
	CodeGenerationChanged kernel.Code = "graph_generation_changed"
	// CodeNotReadable is a read at a change number the readers may not see yet.
	CodeNotReadable kernel.Code = "graph_not_readable"
	// CodeWaitTimeout is a wait for a change number that did not become readable in time.
	CodeWaitTimeout kernel.Code = "graph_wait_timeout"
	// CodeSealNotReady is a seal while a chunked apply is open.
	CodeSealNotReady kernel.Code = "graph_seal_not_ready"
	// CodeHashMismatch is a graph hash that the full recomputation does not reproduce.
	CodeHashMismatch kernel.Code = "graph_hash_mismatch"
	// CodeKGNotFound is a KG or a version that does not exist.
	CodeKGNotFound kernel.Code = "graph_kg_not_found"
)

var codeMeanings = map[kernel.Code]string{
	CodeBatchInvalid:      "a change batch breaks a rule of the registry or of its own shape",
	CodeBindingMissing:    "no binding of the link type allows a link between these two types",
	CodeCardinality:       "a link breaks the cardinality of its binding",
	CodeDanglingLink:      "a link end is not live, or an object retires while a link still ends at it",
	CodeVolatileInRow:     "a volatile attribute was put in a versioned row",
	CodeVolatileOmitted:   "past reads omit volatile values",
	CodeIdentifierTaken:   "the identifier already names another object in that scope",
	CodeHeadMoved:         "the expected head is not the current head",
	CodeLeaseHeld:         "another owner holds an unexpired lease",
	CodeLeaseLost:         "the lease expired or was taken over",
	CodeSemanticTooLow:    "the engine is older than the one that applied last",
	CodeApplyInProgress:   "another apply of the KG is half done",
	CodeGenerationChanged: "the KG moved to a newer generation",
	CodeNotReadable:       "that change number is not readable yet",
	CodeWaitTimeout:       "the change number did not become readable in time",
	CodeSealNotReady:      "a seal needs applied_seq to equal readable_through",
	CodeHashMismatch:      "the kept graph hash differs from the full recomputation",
	CodeKGNotFound:        "no such KG or version",
}

// Codes lists the codes of this unit, sorted.
func Codes() []kernel.Code {
	out := make([]kernel.Code, 0, len(codeMeanings))
	for c := range codeMeanings {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// CodeMeaning is the one-line meaning of a code of this unit, or false.
func CodeMeaning(code kernel.Code) (string, bool) {
	m, ok := codeMeanings[code]
	return m, ok
}
