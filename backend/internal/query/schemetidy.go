package query

import "github.com/ysk6506/praxium/backend/internal/model"

// tidyIdentifier writes the value of an identifier the way the scheme keeps it, so a value typed as it is written in
// a ledger (a MAC address with colons) finds the identifier that was stored tidied (twelve hex digits). A value the
// normalizer refuses is looked up as it is.
func tidyIdentifier(scheme model.IdentifierScheme, value string) string {
	if v, err := model.NormalizeValue(scheme.Normalizer, value); err == nil && v != "" {
		return v
	}
	return value
}
