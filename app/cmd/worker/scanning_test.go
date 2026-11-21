package main

import (
	"context"
	"testing"

	securityapp "github.com/sanctumlabs/fupi/app/internal/application/security"
	"github.com/sanctumlabs/fupi/app/internal/domain/url"
	"github.com/sanctumlabs/fupi/app/internal/ports"
	"github.com/sanctumlabs/fupi/app/pkg/infra/queue/kafka"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubScanner struct{ scanned []string }

func (s *stubScanner) Provider() string { return "stub" }
func (s *stubScanner) Scan(_ context.Context, rawURL string) (ports.ScanResult, error) {
	s.scanned = append(s.scanned, rawURL)
	return ports.ScanResult{Provider: "stub", Verdict: ports.ScanSafe}, nil
}

type stubScans struct{ saved []string }

func (s *stubScans) ExistsByUrlAndProvider(context.Context, string, string) (bool, error) {
	return false, nil
}
func (s *stubScans) Save(_ context.Context, urlID string, _ ports.ScanResult) error {
	s.saved = append(s.saved, urlID)
	return nil
}

type stubSuspender struct{}

func (stubSuspender) Suspend(context.Context, string, url.SuspensionReason) error { return nil }

// A record the outbox relay publishes carries the event type in its headers and the domain event as JSON in its value.
func TestScanHandler_HandsTheRelayedEventToTheSecurityService(t *testing.T) {
	scanner, scans := &stubScanner{}, &stubScans{}
	handler := scanHandler(securityapp.NewService(scanner, scans, stubSuspender{}))

	err := handler(context.Background(), kafka.Delivery{
		Topic: "url.events",
		Value: []byte(`{"URLID":"018f4b0e-0000-7000-8000-000000000001","UserID":"u","OriginalURL":"https://example.com/x","ShortCode":"000abc"}`),
		Headers: []kafka.Header{
			{Key: "event_id", Value: "e-1"},
			{Key: "event_type", Value: "url.created"},
			{Key: "content-type", Value: "application/json"},
		},
	})

	require.NoError(t, err)
	assert.Equal(t, []string{"https://example.com/x"}, scanner.scanned)
	assert.Equal(t, []string{"018f4b0e-0000-7000-8000-000000000001"}, scans.saved)
}

func TestScanHandler_IgnoresOtherEventsOnTheTopic(t *testing.T) {
	scanner := &stubScanner{}
	handler := scanHandler(securityapp.NewService(scanner, &stubScans{}, stubSuspender{}))

	err := handler(context.Background(), kafka.Delivery{
		Value:   []byte(`{"URLID":"018f4b0e-0000-7000-8000-000000000001"}`),
		Headers: []kafka.Header{{Key: "event_type", Value: "url.deleted"}},
	})

	require.NoError(t, err)
	assert.Empty(t, scanner.scanned)
}
