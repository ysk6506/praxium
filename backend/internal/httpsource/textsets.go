package httpsource

// The small sets of texts the reading of the authentication uses.

func containsText(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}

func unionOf(a, b []string) []string {
	out := append([]string{}, a...)
	for _, s := range b {
		if !containsText(out, s) {
			out = append(out, s)
		}
	}
	return out
}
