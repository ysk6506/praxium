package model

// RequireStandardForTest lets the tests of package model_test reach requireStandard.
func RequireStandardForTest(d Definitions) error { return requireStandard(d) }
