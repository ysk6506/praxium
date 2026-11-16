package secrets

import "github.com/ysk6506/praxium/backend/internal/kernel"

// The codes of this unit, beside the kernel's general ones (CodeInvalidValue, CodeNotFound, CodeConflict).
const (
	// CodeScopeDenied is an attempt to open a secret for a source that is not in its scope.
	CodeScopeDenied kernel.Code = "secret_scope_denied"
	// CodeAddressDenied is an attempt to open a secret for an address that is not among its allowed addresses.
	CodeAddressDenied kernel.Code = "secret_address_denied"
	// CodeVersionRetired is an attempt to open a secret that has no version left: every version is retired.
	CodeVersionRetired kernel.Code = "secret_version_retired"
	// CodeIntegrity is a stored secret that does not authenticate: the file was changed, a ciphertext was moved to
	// another secret or version, or the key file has the same id as the key that sealed the secret but other bytes.
	CodeIntegrity kernel.Code = "secret_integrity_failed"
	// CodeMasterKeyMissing is a use of the store without a master key.
	CodeMasterKeyMissing kernel.Code = "secret_master_key_missing"
	// CodeMasterKeyInvalid is a master key file that cannot be read: damaged, of another format or of a version this
	// program does not read.
	CodeMasterKeyInvalid kernel.Code = "secret_master_key_invalid"
	// CodeMasterKeyMismatch is a stored secret that another master key sealed than the one given.
	CodeMasterKeyMismatch kernel.Code = "secret_master_key_mismatch"
)
