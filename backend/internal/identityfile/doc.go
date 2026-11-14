// Package identityfile is the file store of the identity data (ADR-0044): all principals and token hashes in one
// versioned JSON file of the data folder of the built-in file store. Every change rewrites the file atomically (a
// temporary file, synced, then renamed over the old one, then the folder synced), so a crash leaves the old file or
// the new one and never half of one. The unit knows no operating system: the folder comes in through the Disk
// interface, which the composition root implements with files of mode 0600 and the tests implement in memory.
package identityfile
