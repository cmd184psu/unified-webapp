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
// honouring the settings' TLS options: CAFile adds a private root (for example
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
