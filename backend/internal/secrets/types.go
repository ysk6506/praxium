package secrets

import (
	"context"
	"time"
)

// Kind is the kind of a secret: it fixes which fields the secret has.
type Kind string

// The kinds this store keeps. The others of the design (certificate, connection string, keys) come with the changes
// that need them.
const (
	// KindPassword is a user name and a password.
	KindPassword Kind = "password"
	// KindToken is a token.
	KindToken Kind = "token"
)

var kindFields = map[Kind][]string{
	KindPassword: {"username", "password"},
	KindToken:    {"token"},
}

// FieldNames returns the field names of a kind in their order, or false for a kind this store does not know.
func FieldNames(kind Kind) ([]string, bool) {
	names, ok := kindFields[kind]
	return append([]string(nil), names...), ok
}

// Address is a host and a port that a secret may be used for. The host is a name or an IP address in the form
// NormalizeHost gives; the port is 1 to 65535.
type Address struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

// Purpose is what an open is for.
type Purpose string

// The purposes of an open. They are checked to be one of these and are carried for the use records of a later change.
const (
	// PurposeRun is a run of the ingestion of a source.
	PurposeRun Purpose = "run"
	// PurposeCheck is a check of the connection.
	PurposeCheck Purpose = "check"
	// PurposeDiscover is the discovery of what a source offers.
	PurposeDiscover Purpose = "discover"
)

// Use is what a secret is opened for: which source, to which address, for what.
type Use struct {
	SourceID string
	Host     string
	Port     int
	Purpose  Purpose
}

// VersionInfo is what is known about one version without opening it.
type VersionInfo struct {
	Version   int        `json:"version"`
	CreatedAt time.Time  `json:"created_at"`
	RetiredAt *time.Time `json:"retired_at,omitempty"`
}

// Meta is a secret as every role may see it. It never holds a value.
type Meta struct {
	ID           string        `json:"id"`
	Kind         Kind          `json:"kind"`
	FieldNames   []string      `json:"field_names"`
	Version      int           `json:"version"` // the version an open gives; 0 when every version is retired
	Versions     []VersionInfo `json:"versions"`
	CreatedAt    time.Time     `json:"created_at"`
	Scope        []string      `json:"scope"`
	AllowedHosts []Address     `json:"allowed_hosts"`
}

// CreateInput is a new secret with its first version. An empty scope or an empty list of addresses means that no
// source or no address may use the secret yet.
type CreateInput struct {
	ID           string
	Kind         Kind
	Fields       Fields
	Scope        []string
	AllowedHosts []Address
}

// Writer is the port that changes secrets. Every change but Delete needs the master key. The caller checks that no source refers to a secret before it deletes it
// or retires its last version (the question is for the unit that joins the sources to this one).
type Writer interface {
	// Create makes a secret with version 1. A secret with that id is a conflict.
	Create(ctx context.Context, in CreateInput) (Meta, error)
	// AddVersion adds the next version; the open gives it from then on.
	AddVersion(ctx context.Context, id string, fields Fields) (Meta, error)
	// RetireVersion retires a version: its ciphertext is deleted and the retirement is bound by the tag under the
	// master key, so it needs the key. Only a copy of the whole record from before the retirement brings it back.
	RetireVersion(ctx context.Context, id string, version int) (Meta, error)
	// Delete removes the secret with all its versions.
	Delete(ctx context.Context, id string) error
	// SetScope replaces the source ids that may use the secret.
	SetScope(ctx context.Context, id string, sourceIDs []string) (Meta, error)
	// SetAllowedHosts replaces the addresses the secret may be used for.
	SetAllowedHosts(ctx context.Context, id string, hosts []Address) (Meta, error)
}

// Opener is the port that decrypts. It is given to the roles that connect to sources and to no other.
type Opener interface {
	// Open checks the use against the scope and the allowed addresses of the secret and returns the fields of the
	// current version. The caller closes the result when the use ends.
	Open(ctx context.Context, id string, use Use) (*Opened, error)
}

// Describer is the port that reads what is not secret. No answer of it holds a value.
type Describer interface {
	// List returns every secret, in the order of the ids.
	List(ctx context.Context) ([]Meta, error)
	// Get returns one secret, or a not_found error.
	Get(ctx context.Context, id string) (Meta, error)
}
