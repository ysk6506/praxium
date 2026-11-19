package egress

import (
	"crypto/tls"
	"crypto/x509"
)

// TLSConfig makes the TLS settings of a source. The version is 1.2 or later and the certificate and the host
// name are always verified: there is no setting that turns it off. With an empty caPEM the system's list of
// authorities is used. With a caPEM only the certificates in it are trusted, and the list of the system is not
// (a private authority of one source is not trusted for the others, and a public one is not trusted for it).
// The error does not hold the PEM text.
func TLSConfig(caPEM string) (*tls.Config, error) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if caPEM == "" {
		return cfg, nil
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(caPEM)) {
		return nil, &Error{Kind: KindTLS, Reason: "ca_pem holds no certificate that can be read"}
	}
	cfg.RootCAs = pool
	return cfg, nil
}
