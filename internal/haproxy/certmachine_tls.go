package haproxy

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"time"
)

// CertMachineHTTPClient builds the HTTP client the CertMachine client uses,
// honoring the settings' TLS options: CAFile adds a private root (for example
// CertMachine's own CA when CertMachine runs on another machine) on top of the
// system roots, and InsecureSkipVerify is the documented, off-by-default toggle.
//
// This is the ONE file in the package that imports crypto/x509, and only to
// build a TLS trust pool. It does not read, parse or report anything about any
// certificate's contents (D15): cert details always come from CertMachine over
// REST. TestCertPackageHasNoX509OrOpenSSLImport allows exactly this file.
func CertMachineHTTPClient(s CertMachineSettings) (*http.Client, error) {
	cfg := &tls.Config{InsecureSkipVerify: s.InsecureSkipVerify} //nolint:gosec // documented off-by-default toggle
	if s.CAFile != "" {
		pem, err := os.ReadFile(s.CAFile)
		if err != nil {
			return nil, fmt.Errorf("haproxy: reading certmachine ca_file: %w", err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("haproxy: certmachine ca_file %q contains no usable certificates", s.CAFile)
		}
		cfg.RootCAs = pool
	}
	return &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{TLSClientConfig: cfg}}, nil
}

// StageTLSConfig is the TLS client config the staged test uses to ask the staged
// proxy for serverName: system roots, plus caFile when set. Like the function
// above it only builds a trust pool; it never reads a certificate's contents.
func StageTLSConfig(caFile, serverName string) (*tls.Config, error) {
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if caFile != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("cannot read the CA file %s: %w", caFile, err)
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("the CA file %s holds no certificates", caFile)
		}
	}
	return &tls.Config{RootCAs: pool, ServerName: serverName}, nil
}
