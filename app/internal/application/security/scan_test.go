package securityapp

import (
	"context"
	"errors"
	"testing"

	"github.com/sanctumlabs/fupi/app/internal/domain/url"
	"github.com/sanctumlabs/fupi/app/internal/ports"
	"github.com/sanctumlabs/fupi/app/pkg/errdefs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	urlID   = "018f4b0e-0000-7000-8000-000000000001"
	created = `{"URLID":"018f4b0e-0000-7000-8000-000000000001","UserID":"u","OriginalURL":"https://bad.example/x","ShortCode":"000abc","ExpiresOn":"2030-01-01T00:00:00Z"}`
)

type fakeScanner struct {
	result ports.ScanResult
	err    error
	scans  []string
}

func (f *fakeScanner) Provider() string { return "fake" }
func (f *fakeScanner) Scan(_ context.Context, rawURL string) (ports.ScanResult, error) {
	f.scans = append(f.scans, rawURL)
	result := f.result
	result.Provider = "fake"
	return result, f.err
}

type fakeScans struct {
	stored    map[string]ports.ScanResult
	existsErr error
	saveErr   error
}

func (f *fakeScans) ExistsByUrlAndProvider(_ context.Context, id, provider string) (bool, error) {
	_, ok := f.stored[id+"/"+provider]
	return ok, f.existsErr
}

func (f *fakeScans) Save(_ context.Context, id string, result ports.ScanResult) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.stored[id+"/"+result.Provider] = result
	return nil
}

type suspension struct {
	id     string
	reason url.SuspensionReason
}

type fakeSuspender struct {
	err  error
	done []suspension
}

func (f *fakeSuspender) Suspend(_ context.Context, id string, reason url.SuspensionReason) error {
	if f.err != nil {
		return f.err
	}
	f.done = append(f.done, suspension{id, reason})
	return nil
}

func newTestService(verdict ports.ScanVerdict) (*Service, *fakeScanner, *fakeScans, *fakeSuspender) {
	scanner := &fakeScanner{result: ports.ScanResult{Verdict: verdict, RawResponse: []byte(`{}`)}}
	scans := &fakeScans{stored: map[string]ports.ScanResult{}}
	suspender := &fakeSuspender{}
	return NewService(scanner, scans, suspender), scanner, scans, suspender
}

func createdEvent() Event {
	return Event{ID: "evt-1", Type: "url.created", Payload: []byte(created)}
}

func TestHandle_AFlaggedUrlIsSuspendedWithTheMatchingReason(t *testing.T) {
	for verdict, reason := range map[ports.ScanVerdict]url.SuspensionReason{
		ports.ScanMalware:  url.SuspensionReasonMalware,
		ports.ScanPhishing: url.SuspensionReasonPhishing,
	} {
		svc, scanner, scans, suspender := newTestService(verdict)

		require.NoError(t, svc.Handle(context.Background(), createdEvent()))

		assert.Equal(t, []string{"https://bad.example/x"}, scanner.scans)
		assert.Equal(t, []suspension{{urlID, reason}}, suspender.done)
		assert.Equal(t, verdict, scans.stored[urlID+"/fake"].Verdict, "the scan is recorded")
	}
}

func TestHandle_ASafeOrUnknownResultSuspendsNothingButIsRecorded(t *testing.T) {
	for _, verdict := range []ports.ScanVerdict{ports.ScanSafe, ports.ScanUnknown} {
		svc, _, scans, suspender := newTestService(verdict)

		require.NoError(t, svc.Handle(context.Background(), createdEvent()))

		assert.Empty(t, suspender.done)
		assert.Equal(t, verdict, scans.stored[urlID+"/fake"].Verdict)
	}
}

func TestHandle_ARedeliveredEventIsNotScannedOrSuspendedTwice(t *testing.T) {
	svc, scanner, _, suspender := newTestService(ports.ScanMalware)

	require.NoError(t, svc.Handle(context.Background(), createdEvent()))
	require.NoError(t, svc.Handle(context.Background(), createdEvent()))

	assert.Len(t, scanner.scans, 1)
	assert.Len(t, suspender.done, 1)
}

func TestHandle_AScannerOutageIsAnErrorSoTheEventIsRetriedAndNothingIsSuspendedOrRecorded(t *testing.T) {
	svc, scanner, scans, suspender := newTestService(ports.ScanMalware)
	scanner.err = ports.ErrScannerUnavailable

	err := svc.Handle(context.Background(), createdEvent())

	require.ErrorIs(t, err, ports.ErrScannerUnavailable)
	assert.Empty(t, suspender.done)
	assert.Empty(t, scans.stored)
}

func TestHandle_AUrlThatCannotBeSuspendedAnymoreStillHasItsScanRecorded(t *testing.T) {
	for name, suspendErr := range map[string]error{
		"already not active": errdefs.Conflict(errors.New("not active")),
	} {
		t.Run(name, func(t *testing.T) {
			svc, _, scans, suspender := newTestService(ports.ScanMalware)
			suspender.err = suspendErr

			require.NoError(t, svc.Handle(context.Background(), createdEvent()))

			assert.Contains(t, scans.stored, urlID+"/fake")
		})
	}
}

func TestHandle_AUrlThatIsGoneIsSkippedWithoutAnError(t *testing.T) {
	t.Run("gone when suspending", func(t *testing.T) {
		svc, _, scans, suspender := newTestService(ports.ScanMalware)
		suspender.err = errdefs.NotFound(errors.New("no rows"))

		require.NoError(t, svc.Handle(context.Background(), createdEvent()))
		assert.Empty(t, scans.stored)
	})
	t.Run("gone when recording", func(t *testing.T) {
		svc, _, scans, _ := newTestService(ports.ScanSafe)
		scans.saveErr = errdefs.NotFound(errors.New("foreign key"))

		require.NoError(t, svc.Handle(context.Background(), createdEvent()))
	})
}

func TestHandle_AnUnexpectedSuspendOrStoreFailureIsRetried(t *testing.T) {
	svc, _, _, suspender := newTestService(ports.ScanMalware)
	suspender.err = errors.New("connection refused")
	require.Error(t, svc.Handle(context.Background(), createdEvent()))

	svc, _, scans, _ := newTestService(ports.ScanSafe)
	scans.saveErr = errors.New("connection refused")
	require.Error(t, svc.Handle(context.Background(), createdEvent()))

	svc, _, scans, _ = newTestService(ports.ScanSafe)
	scans.existsErr = errors.New("connection refused")
	require.Error(t, svc.Handle(context.Background(), createdEvent()))
}

func TestHandle_OtherEventsAreIgnored(t *testing.T) {
	svc, scanner, _, _ := newTestService(ports.ScanMalware)

	require.NoError(t, svc.Handle(context.Background(), Event{ID: "e", Type: "url.expired", Payload: []byte(created)}))

	assert.Empty(t, scanner.scans)
}

func TestHandle_APoisonPayloadIsSkippedNotRetried(t *testing.T) {
	for name, payload := range map[string]string{
		"not json":         `{{{`,
		"no url id":        `{"OriginalURL":"https://bad.example/"}`,
		"malformed url id": `{"URLID":"nope","OriginalURL":"https://bad.example/"}`,
		"no original url":  `{"URLID":"018f4b0e-0000-7000-8000-000000000001"}`,
		"empty":            ``,
	} {
		t.Run(name, func(t *testing.T) {
			svc, scanner, _, _ := newTestService(ports.ScanMalware)

			require.NoError(t, svc.Handle(context.Background(), Event{ID: "e", Type: "url.created", Payload: []byte(payload)}))

			assert.Empty(t, scanner.scans)
		})
	}
}
