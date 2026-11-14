// Package definitionsfile is the file store of the definitions (ADR-0049): everything in one versioned JSON file,
// definitions.json, in the data folder of the built-in file store. Every write puts the whole file anew (a temporary
// file, synced, then renamed over the old one, then the folder synced), so a crash leaves the old file or the new one
// and never half of one. The journal and the snapshots of the file store of the KG are not touched. The unit knows no
// operating system: the folder comes in through the Disk interface, which the composition root implements with files
// that only their owner can read, and the tests implement in memory.
package definitionsfile
