// Package securityapp holds the Security use cases: scanning the urls that were created and suspending the ones a scanner
// flags.
package securityapp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/sanctumlabs/fupi/app/internal/domain/url"
	"github.com/sanctumlabs/fupi/app/internal/ports"
	"github.com/sanctumlabs/fupi/app/pkg/errdefs"
)

const urlCreatedEventType = "url.created"

// Event is a domain event as the Security context receives it from the broker.
type Event struct {
	// ID is the event's id (the event_id header), for the log.
	ID string
	// Type is the event type, such as url.created.
	Type string
	// Payload is the event as JSON.
	Payload []byte
}

// urlCreated is the part of the url.created payload the scan needs. The payload is the URLCreated domain event
// marshalled as it is, so its keys are the Go field names.
type urlCreated struct {
	URLID       string
	OriginalURL string
}

// URLSuspender suspends a url. It reports a url that is not ACTIVE as a Conflict and an unknown one as NotFound.
type URLSuspender interface {
	Suspend(ctx context.Context, id string, reason url.SuspensionReason) error
}

// Service exposes the Security use cases.
type Service struct {
	scanner ports.URLScanner
	scans   ports.UrlScanDatastore
	urls    URLSuspender
}

// NewService wires the Security use cases to their ports.
func NewService(scanner ports.URLScanner, scans ports.UrlScanDatastore, urls URLSuspender) *Service {
	return &Service{scanner: scanner, scans: scans, urls: urls}
}

// Handle scans the url of a url.created event and suspends it when the scan flags it. Other events are ignored.
//
// An error means the event must be handled again: the scanner could not be reached, or a store failed. A payload that can
// never be handled is logged and reported as success, so it does not block the stream. Handling is idempotent: a url the
// provider already scanned is not scanned again, so a redelivered event neither scans nor suspends twice. The scan is
// recorded last, so an event that failed after the suspension is handled again, finds the url suspended and records it.
func (svc *Service) Handle(ctx context.Context, event Event) error {
	if event.Type != urlCreatedEventType {
		return nil
	}

	var created urlCreated
	if err := json.Unmarshal(event.Payload, &created); err != nil || created.OriginalURL == "" {
		slog.WarnContext(ctx, "Skipping an undecodable url.created event", "event_id", event.ID, "error", err)
		return nil
	}
	if _, err := uuid.Parse(created.URLID); err != nil {
		slog.WarnContext(ctx, "Skipping a url.created event without a valid url id", "event_id", event.ID)
		return nil
	}

	provider := svc.scanner.Provider()
	scanned, err := svc.scans.ExistsByUrlAndProvider(ctx, created.URLID, provider)
	if err != nil {
		return err
	}
	if scanned {
		slog.InfoContext(ctx, "The url was scanned already, skipping", "event_id", event.ID, "url_id", created.URLID, "provider", provider)
		return nil
	}

	result, err := svc.scanner.Scan(ctx, created.OriginalURL)
	if err != nil {
		return fmt.Errorf("scan url %s: %w", created.URLID, err)
	}

	if reason, flagged := suspensionReason(result.Verdict); flagged {
		suspendErr := svc.urls.Suspend(ctx, created.URLID, reason)
		switch {
		case suspendErr == nil:
			slog.WarnContext(ctx, "Suspended a url flagged by the scanner", "url_id", created.URLID, "provider", provider, "reason", reason)
		case errdefs.IsNotFound(suspendErr):
			slog.InfoContext(ctx, "The flagged url is gone, nothing to suspend", "url_id", created.URLID)
			return nil
		case errdefs.IsConflict(suspendErr):
			slog.InfoContext(ctx, "The flagged url is not active, leaving it as it is", "url_id", created.URLID, "provider", provider, "reason", reason)
		default:
			return fmt.Errorf("suspend url %s: %w", created.URLID, suspendErr)
		}
	}

	if saveErr := svc.scans.Save(ctx, created.URLID, result); saveErr != nil {
		if errdefs.IsNotFound(saveErr) {
			slog.InfoContext(ctx, "The scanned url is gone, not recording the scan", "url_id", created.URLID)
			return nil
		}
		return saveErr
	}
	return nil
}

// suspensionReason maps a verdict to the reason to suspend for; a safe or unknown verdict suspends nothing.
func suspensionReason(verdict ports.ScanVerdict) (url.SuspensionReason, bool) {
	switch verdict {
	case ports.ScanMalware:
		return url.SuspensionReasonMalware, true
	case ports.ScanPhishing:
		return url.SuspensionReasonPhishing, true
	}
	return "", false
}
