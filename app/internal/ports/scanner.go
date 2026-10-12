package ports

import (
	"context"
	"errors"
)

// ScanVerdict is what a scanner concluded about a url.
type ScanVerdict string

const (
	// ScanSafe means the provider knows no threat at the url.
	ScanSafe ScanVerdict = "safe"
	// ScanMalware means the url serves malware or other harmful software.
	ScanMalware ScanVerdict = "malware"
	// ScanPhishing means the url is a phishing or social engineering page.
	ScanPhishing ScanVerdict = "phishing"
	// ScanUnknown means the provider answered with something this service does not understand. It suspends nothing.
	ScanUnknown ScanVerdict = "unknown"
)

// ErrScannerUnavailable means the scan could not be done (the provider is down, slow, rate limiting or rejecting the
// request), as opposed to the scan finding nothing. The caller must not treat it as a verdict: nothing is suspended and
// the scan is tried again later.
var ErrScannerUnavailable = errors.New("the url scanner is unavailable")

// ScanResult is the outcome of scanning one url.
type ScanResult struct {
	// Provider names the scanner that produced the result, as stored in url_scans.provider.
	Provider string
	Verdict  ScanVerdict
	// RawResponse is the provider's answer, kept for auditing. It may be nil.
	RawResponse []byte
}

// URLScanner checks a url against a threat database.
type URLScanner interface {
	// Provider names the scanner, as stored in url_scans.provider.
	Provider() string
	// Scan checks the url. An error wraps ErrScannerUnavailable when the scan could not be done.
	Scan(ctx context.Context, rawURL string) (ScanResult, error)
}

// UrlScanDatastore stores the results of url scans.
type UrlScanDatastore interface {
	// ExistsByUrlAndProvider reports whether the provider already scanned the url.
	ExistsByUrlAndProvider(ctx context.Context, urlID, provider string) (bool, error)
	// Save stores the result of scanning the url.
	Save(ctx context.Context, urlID string, result ScanResult) error
}
