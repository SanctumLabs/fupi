// Package safebrowsing implements ports.URLScanner on the Google Safe Browsing v4 Lookup API (threatMatches:find).
package safebrowsing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sanctumlabs/fupi/app/internal/ports"
)

const (
	// Provider is how url_scans names this scanner.
	Provider = "google_safe_browsing"

	defaultBaseURL = "https://safebrowsing.googleapis.com"
	findPath       = "/v4/threatMatches:find"
	clientID       = "fupi"

	defaultTimeout     = 5 * time.Second
	defaultAttempts    = 3
	defaultBackoff     = 500 * time.Millisecond
	maxResponseBytes   = 1 << 20
	apiKeyHeader       = "X-Goog-Api-Key"
	threatMalware      = "MALWARE"
	threatUnwanted     = "UNWANTED_SOFTWARE"
	threatHarmfulApp   = "POTENTIALLY_HARMFUL_APPLICATION"
	threatSocialEngine = "SOCIAL_ENGINEERING"
)

var _ ports.URLScanner = (*Scanner)(nil)

// Scanner checks urls against Google Safe Browsing.
type Scanner struct {
	apiKey   string
	baseURL  string
	client   *http.Client
	attempts int
	backoff  time.Duration
}

// Option configures a Scanner.
type Option func(*Scanner)

// WithBaseURL sets the API host; it is for tests.
func WithBaseURL(baseURL string) Option { return func(s *Scanner) { s.baseURL = baseURL } }

// WithTimeout bounds each attempt.
func WithTimeout(timeout time.Duration) Option {
	return func(s *Scanner) { s.client.Timeout = timeout }
}

// WithRetries sets how many attempts one scan makes and the wait before the second; it doubles after each.
func WithRetries(attempts int, backoff time.Duration) Option {
	return func(s *Scanner) { s.attempts, s.backoff = attempts, backoff }
}

// NewScanner builds a Scanner that authenticates with apiKey. The key travels in a header, never in the url, so an error
// or a log line that carries the request url cannot leak it.
func NewScanner(apiKey string, opts ...Option) *Scanner {
	s := &Scanner{
		apiKey:   apiKey,
		baseURL:  defaultBaseURL,
		client:   &http.Client{Timeout: defaultTimeout},
		attempts: defaultAttempts,
		backoff:  defaultBackoff,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Provider names the scanner.
func (s *Scanner) Provider() string { return Provider }

type findRequest struct {
	Client     clientInfo `json:"client"`
	ThreatInfo threatInfo `json:"threatInfo"`
}

type clientInfo struct {
	ClientID      string `json:"clientId"`
	ClientVersion string `json:"clientVersion"`
}

type threatInfo struct {
	ThreatTypes      []string      `json:"threatTypes"`
	PlatformTypes    []string      `json:"platformTypes"`
	ThreatEntryTypes []string      `json:"threatEntryTypes"`
	ThreatEntries    []threatEntry `json:"threatEntries"`
}

type threatEntry struct {
	URL string `json:"url"`
}

type findResponse struct {
	Matches []struct {
		ThreatType string `json:"threatType"`
	} `json:"matches"`
}

// Scan asks Safe Browsing about the url. Transient failures (network errors, timeouts, 429 and 5xx) are retried a bounded
// number of times; any failure that remains wraps ports.ErrScannerUnavailable and is never a verdict.
func (s *Scanner) Scan(ctx context.Context, rawURL string) (ports.ScanResult, error) {
	payload, err := json.Marshal(findRequest{
		Client: clientInfo{ClientID: clientID, ClientVersion: "1.0"},
		ThreatInfo: threatInfo{
			ThreatTypes:      []string{threatMalware, threatSocialEngine, threatUnwanted, threatHarmfulApp},
			PlatformTypes:    []string{"ANY_PLATFORM"},
			ThreatEntryTypes: []string{"URL"},
			ThreatEntries:    []threatEntry{{URL: rawURL}},
		},
	})
	if err != nil {
		return ports.ScanResult{}, fmt.Errorf("encode the safe browsing request: %w", err)
	}

	var lastErr error
	wait := s.backoff
	for attempt := 1; attempt <= s.attempts; attempt++ {
		body, retry, attemptErr := s.find(ctx, payload)
		if attemptErr == nil {
			return toResult(body)
		}
		lastErr = attemptErr
		if !retry || attempt == s.attempts {
			break
		}
		select {
		case <-ctx.Done():
			return ports.ScanResult{}, fmt.Errorf("%w: %w", ports.ErrScannerUnavailable, ctx.Err())
		case <-time.After(wait):
		}
		wait *= 2
	}
	return ports.ScanResult{}, fmt.Errorf("%w: %w", ports.ErrScannerUnavailable, lastErr)
}

// find makes one attempt. retry says whether trying again can help.
func (s *Scanner) find(ctx context.Context, payload []byte) (body []byte, retry bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+findPath, bytes.NewReader(payload))
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(apiKeyHeader, s.apiKey)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, ctx.Err() == nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBytes))
		retriable := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError
		return nil, retriable, fmt.Errorf("safe browsing answered %d", resp.StatusCode)
	}

	body, err = io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, true, err
	}
	return body, false, nil
}

// toResult turns the API answer into a verdict. No matches means safe; the worst threat wins when there are several.
func toResult(body []byte) (ports.ScanResult, error) {
	var response findResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return ports.ScanResult{}, fmt.Errorf("%w: undecodable answer: %w", ports.ErrScannerUnavailable, err)
	}

	verdict := ports.ScanSafe
	if len(response.Matches) > 0 {
		verdict = ports.ScanUnknown
	}
	for _, match := range response.Matches {
		switch match.ThreatType {
		case threatMalware, threatUnwanted, threatHarmfulApp:
			verdict = ports.ScanMalware
		case threatSocialEngine:
			if verdict != ports.ScanMalware {
				verdict = ports.ScanPhishing
			}
		}
	}
	return ports.ScanResult{Provider: Provider, Verdict: verdict, RawResponse: body}, nil
}
