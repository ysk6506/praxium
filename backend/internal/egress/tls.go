package egress

import (
	"crypto/tls"
	"crypto/x509"
)

// tlsConfig makes the TLS settings of a source. The version is 1.2 or later and the certificate and the host
// name are always verified: there is no setting that turns it off. With an empty caPEM the system's list of
// authorities is used. With a caPEM only the certificates in it are trusted, and the list of the system is not
// (a private authority of one source is not trusted for the others, and a public one is not trusted for it).
// The error does not hold the PEM text. The function is not exported on purpose: nothing outside this package holds the
// settings of TLS, so no one else can change a field of them (RegisterPostgresTLS is the way to hand them to the driver).
//
// A source whose certificate cannot be verified is not given a way to skip it here: it names the keys it accepts
// (pins), and the settings for that are made in pin.go, from the pins and from nothing else.
func tlsConfig(caPEM string) (*tls.Config, error) {
	var pool *x509.CertPool
	if caPEM != "" {
		pool = x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(caPEM)) {
			return nil, &Error{Kind: KindTLS, Reason: "ca_pem holds no certificate that can be read"}
		}
	}
	return &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: pool}, nil
}

// RegisterPostgresTLS makes the TLS settings of a PostgreSQL connection (verified against caPEM, or pinned when pins are
// given; the two are not used together) and hands them to register under key, which is the function the driver has for
// it. The caller never holds the settings, so it has no way to change them. The returned function takes them out of the
// driver's list again.
func RegisterPostgresTLS(register func(key string, cfg *tls.Config) error, key, caPEM string, pins []string) (unregister func(), err error) {
	cfg, err := connectionTLS(caPEM, pins)
	if err != nil {
		return nil, err
	}
	if err := register(key, cfg); err != nil {
		return nil, &Error{Kind: KindTLS, Reason: "the TLS settings could not be registered with the driver"}
	}
	return func() { _ = register(key, nil) }, nil
}

// connectionTLS is the settings of a connection: pinned when there are pins, verified (against caPEM when it is given)
// when there are not. Pins and a certificate authority are not used together: the pins replace the verification.
func connectionTLS(caPEM string, pins []string) (*tls.Config, error) {
	if len(pins) == 0 {
		return tlsConfig(caPEM)
	}
	if caPEM != "" {
		return nil, &Error{Kind: KindTLS, Reason: "ca_pem and tls.pins are not used together"}
	}
	return pinnedConfig(pins)
}
