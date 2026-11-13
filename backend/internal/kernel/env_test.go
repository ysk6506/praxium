package kernel

import "testing"

// [REQ-001]
func TestLookupEnvReadsTheEnvironmentOnlyHere(t *testing.T) {
	t.Setenv("PRAXIUM_KERNEL_ENV_TEST", "value")
	if got, ok := LookupEnv("PRAXIUM_KERNEL_ENV_TEST"); !ok || got != "value" {
		t.Fatalf("LookupEnv = %q, %v", got, ok)
	}
	if got, ok := LookupEnv("PRAXIUM_KERNEL_ENV_TEST_UNSET"); ok || got != "" {
		t.Fatalf("unset variable = %q, %v", got, ok)
	}
}
