package kernel

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"os"
	"time"
)

// This file is the only place in the backend that reads the wall clock,
// system randomness or the environment (REQ-001). The structure check lists
// it as the one designated file; every other file receives a Clock, an Rng or
// a lookup function.

// SystemClock is the Clock of the real world, in UTC.
type SystemClock struct{}

// Now returns the current wall-clock time in UTC.
func (SystemClock) Now() time.Time { return time.Now().UTC() }

// SystemSeed returns 64 unpredictable bits from the operating system.
func SystemSeed() uint64 {
	var raw [8]byte
	// Since Go 1.24 Read never returns an error: it ends the program if the
	// system source of randomness fails.
	_, _ = rand.Read(raw[:])
	return binary.BigEndian.Uint64(raw[:])
}

// NewSystemRng is a SeededRng started from SystemSeed, for runs that did not
// ask for a seed. Log the seed (Seed) to be able to replay the run.
func NewSystemRng() *SeededRng { return NewSeededRng(SystemSeed()) }

// NewToken returns an unguessable token of n random bytes as 2n hex
// characters, for share links and per-run secrets. It does not use an Rng:
// a token must never be reproducible.
func NewToken(n int) string {
	raw := make([]byte, n)
	_, _ = rand.Read(raw)
	return hex.EncodeToString(raw)
}

// LookupEnv reads one environment variable. The command line client and the
// composition root pass this function (or a fixed one in tests) to the code
// that needs configuration, so the environment is read here and nowhere else.
func LookupEnv(name string) (string, bool) { return os.LookupEnv(name) }
