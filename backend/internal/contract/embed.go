package contract

import (
	"bytes"
	"embed"
	"encoding/json"
	"io/fs"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// files holds the contract as the repository keeps it:
//
//	openapi/parts/*.json     the hand-written parts, assembled by Assemble
//	openapi/openapi.json     the assembled document (a build product, never edited by hand)
//	planned/routes.json      the routes designed but not yet in the contract
//
//go:embed openapi planned
var files embed.FS

// PartsDir is the folder of the parts inside Files.
const PartsDir = "openapi/parts"

// AssembledPath is where the assembled document lives, from the root of the unit's folder.
const AssembledPath = "openapi/openapi.json"

// Files returns the embedded contract folder, for Assemble and for tests.
func Files() fs.FS { return files }

// OpenAPI returns the assembled contract document as committed, which is what
// `GET /v1/contract` serves. The caller must not change the bytes.
func OpenAPI() []byte {
	data, err := files.ReadFile(AssembledPath)
	if err != nil {
		return nil
	}
	return data
}

// PlannedRoute is a route that the design names and the contract does not yet
// specify. Keeping them in a registry keeps the gap visible and checked.
type PlannedRoute struct {
	Method  string `json:"method"`
	Path    string `json:"path"`
	Feature string `json:"feature"`
	ADR     string `json:"adr"`
	Phase   string `json:"phase"`
	Status  string `json:"status"`
}

// PlannedRoutes reads the registry of planned routes.
func PlannedRoutes() ([]PlannedRoute, error) {
	data, err := files.ReadFile("planned/routes.json")
	if err != nil {
		return nil, kernel.Wrap(kernel.CodeInternal, err, "the planned route registry is missing")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var reg struct {
		Version int            `json:"version"`
		Routes  []PlannedRoute `json:"routes"`
	}
	if err := dec.Decode(&reg); err != nil {
		return nil, kernel.Wrap(kernel.CodeInvalidValue, err, "the planned route registry is not valid")
	}
	return reg.Routes, nil
}
