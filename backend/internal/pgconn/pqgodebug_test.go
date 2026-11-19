package pgconn

import (
	"strings"
	"testing"
)

// [REQ-678] The driver writes its messages, the password among them, to the standard error when PQGO_DEBUG is set, so a
// server that has it set does not connect.
func TestPqgoDebugStopsTheConnection(t *testing.T) {
	t.Setenv(driverDebugEnv, "1")
	_, err := newConnector("host=db.example.test user=u dbname=d sslmode=disable", false, 0)
	if err == nil || !strings.Contains(err.Error(), driverDebugEnv) {
		t.Fatalf("%v", err)
	}
	found := false
	for _, n := range driverEnvNames() {
		found = found || n == driverDebugEnv
	}
	if !found {
		t.Fatal("PQGO_DEBUG is not among the refused variables")
	}
}
