// Package secretsfile is the file store of the secrets (ADR-0071): everything in one versioned JSON file, secrets.json,
// in the data folder. The file holds only ciphertext, wrapped keys and the tags of the bindings (never a value in the
// clear), but it is still a file only its owner may read. Every write puts the whole file anew (a temporary file,
// synced, then renamed over the old one, then the folder synced), so a crash leaves the old file or the new one and
// never half of one. The unit knows no operating system: the folder comes in through the Disk interface, which the
// composition root implements with files that only their owner can read, and the tests implement in memory.
package secretsfile
