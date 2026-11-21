package safebrowsing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sanctumlabs/fupi/app/internal/ports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestScanner(t *testing.T, handler http.HandlerFunc) *Scanner {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return NewScanner("secret-key", WithBaseURL(server.URL), WithRetries(3, time.Millisecond), WithTimeout(time.Second))
}

func TestScan_ASafeUrlHasNoMatches(t *testing.T) {
	scanner := newTestScanner(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{}`)) })

	result, err := scanner.Scan(context.Background(), "https://example.com/")

	require.NoError(t, err)
	assert.Equal(t, ports.ScanSafe, result.Verdict)
	assert.Equal(t, "google_safe_browsing", result.Provider)
	assert.Equal(t, "google_safe_browsing", scanner.Provider())
}

func TestScan_SendsTheUrlAndTheKeyInAHeaderNotInTheUrl(t *testing.T) {
	var gotPath, gotKey string
	var gotQuery string
	var body struct {
		ThreatInfo struct {
			ThreatEntries []struct {
				URL string `json:"url"`
			} `json:"threatEntries"`
		} `json:"threatInfo"`
	}
	scanner := newTestScanner(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotKey, gotQuery = r.URL.Path, r.Header.Get("X-Goog-Api-Key"), r.URL.RawQuery
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		_, _ = w.Write([]byte(`{}`))
	})

	_, err := scanner.Scan(context.Background(), "https://example.com/page")

	require.NoError(t, err)
	assert.Equal(t, "/v4/threatMatches:find", gotPath)
	assert.Equal(t, "secret-key", gotKey)
	assert.Empty(t, gotQuery, "the key must not travel in the url, where errors and proxies log it")
	require.Len(t, body.ThreatInfo.ThreatEntries, 1)
	assert.Equal(t, "https://example.com/page", body.ThreatInfo.ThreatEntries[0].URL)
}

func TestScan_MapsThreatTypesToVerdicts(t *testing.T) {
	for name, tc := range map[string]struct {
		matches string
		want    ports.ScanVerdict
	}{
		"malware":               {`[{"threatType":"MALWARE"}]`, ports.ScanMalware},
		"unwanted software":     {`[{"threatType":"UNWANTED_SOFTWARE"}]`, ports.ScanMalware},
		"harmful application":   {`[{"threatType":"POTENTIALLY_HARMFUL_APPLICATION"}]`, ports.ScanMalware},
		"social engineering":    {`[{"threatType":"SOCIAL_ENGINEERING"}]`, ports.ScanPhishing},
		"malware beats phish":   {`[{"threatType":"SOCIAL_ENGINEERING"},{"threatType":"MALWARE"}]`, ports.ScanMalware},
		"a type we do not know": {`[{"threatType":"SOMETHING_NEW"}]`, ports.ScanUnknown},
	} {
		t.Run(name, func(t *testing.T) {
			scanner := newTestScanner(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"matches":` + tc.matches + `}`))
			})

			result, err := scanner.Scan(context.Background(), "https://bad.example/")

			require.NoError(t, err)
			assert.Equal(t, tc.want, result.Verdict)
			assert.JSONEq(t, `{"matches":`+tc.matches+`}`, string(result.RawResponse))
		})
	}
}

func TestScan_RetriesTransientFailuresAndThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	scanner := newTestScanner(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	})

	result, err := scanner.Scan(context.Background(), "https://example.com/")

	require.NoError(t, err)
	assert.Equal(t, ports.ScanSafe, result.Verdict)
	assert.Equal(t, int32(3), calls.Load())
}

func TestScan_GivesUpAfterTheRetriesAndReportsTheScannerAsUnavailable(t *testing.T) {
	var calls atomic.Int32
	scanner := newTestScanner(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	})

	_, err := scanner.Scan(context.Background(), "https://example.com/")

	require.ErrorIs(t, err, ports.ErrScannerUnavailable)
	assert.Equal(t, int32(3), calls.Load(), "bounded retries")
}

func TestScan_ARejectedRequestIsNotRetriedButIsStillUnavailable(t *testing.T) {
	var calls atomic.Int32
	scanner := newTestScanner(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusForbidden)
	})

	_, err := scanner.Scan(context.Background(), "https://example.com/")

	require.ErrorIs(t, err, ports.ErrScannerUnavailable)
	assert.Equal(t, int32(1), calls.Load())
}

func TestScan_ASlowProviderTimesOutAsUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	t.Cleanup(server.Close)
	scanner := NewScanner("k", WithBaseURL(server.URL), WithRetries(2, time.Millisecond), WithTimeout(20*time.Millisecond))

	_, err := scanner.Scan(context.Background(), "https://example.com/")

	require.ErrorIs(t, err, ports.ErrScannerUnavailable)
}

func TestScan_AnErrorNeverCarriesTheKey(t *testing.T) {
	scanner := NewScanner("secret-key", WithBaseURL("http://127.0.0.1:1"), WithRetries(1, time.Millisecond), WithTimeout(time.Second))

	_, err := scanner.Scan(context.Background(), "https://example.com/")

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "secret-key")
}

func TestScan_ACancelledContextStopsTheRetries(t *testing.T) {
	var calls atomic.Int32
	scanner := newTestScanner(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := scanner.Scan(ctx, "https://example.com/")

	require.Error(t, err)
	assert.LessOrEqual(t, calls.Load(), int32(1))
}
