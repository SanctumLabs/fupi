package scandatastore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	postgresql "github.com/sanctumlabs/fupi/app/internal/adapters/postgres/sql"
	"github.com/sanctumlabs/fupi/app/internal/core/entity"
	"github.com/sanctumlabs/fupi/app/internal/ports"
	"github.com/sanctumlabs/fupi/app/pkg/errdefs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeQuerier struct {
	exists    bool
	err       error
	existsArg postgresql.QueryUrlScanExistsByUrlIdAndProviderParams
	createArg postgresql.QueryCreateUrlScanParams
}

func (f *fakeQuerier) QueryUrlScanExistsByUrlIdAndProvider(_ context.Context, arg postgresql.QueryUrlScanExistsByUrlIdAndProviderParams) (bool, error) {
	f.existsArg = arg
	return f.exists, f.err
}

func (f *fakeQuerier) QueryCreateUrlScan(_ context.Context, arg postgresql.QueryCreateUrlScanParams) (postgresql.UrlScan, error) {
	f.createArg = arg
	return postgresql.UrlScan{}, f.err
}

func newAdapter(q *fakeQuerier) *Adapter { return &Adapter{queries: q, timeout: time.Second} }

func TestExistsByUrlAndProvider(t *testing.T) {
	id := entity.IDToString(entity.NewID())
	q := &fakeQuerier{exists: true}

	exists, err := newAdapter(q).ExistsByUrlAndProvider(context.Background(), id, "google_safe_browsing")

	require.NoError(t, err)
	assert.True(t, exists)
	assert.True(t, q.existsArg.UrlID.Valid)
	assert.Equal(t, "google_safe_browsing", q.existsArg.Provider)
}

func TestExistsByUrlAndProvider_AMalformedIdIsAnError(t *testing.T) {
	_, err := newAdapter(&fakeQuerier{}).ExistsByUrlAndProvider(context.Background(), "nope", "p")

	require.Error(t, err)
}

func TestSave_StoresTheVerdictAndTheRawResponse(t *testing.T) {
	id := entity.IDToString(entity.NewID())
	q := &fakeQuerier{}

	err := newAdapter(q).Save(context.Background(), id, ports.ScanResult{
		Provider: "google_safe_browsing", Verdict: ports.ScanMalware, RawResponse: []byte(`{"matches":[]}`),
	})

	require.NoError(t, err)
	assert.Equal(t, pgtype.UUID{Bytes: mustParse(t, id), Valid: true}, q.createArg.UrlID)
	assert.Equal(t, "google_safe_browsing", q.createArg.Provider)
	assert.Equal(t, "malware", q.createArg.Result)
	assert.JSONEq(t, `{"matches":[]}`, string(q.createArg.RawResponse))
}

func TestSave_ReturnsTheDatastoreFailure(t *testing.T) {
	id := entity.IDToString(entity.NewID())

	err := newAdapter(&fakeQuerier{err: errors.New("boom")}).Save(context.Background(), id, ports.ScanResult{Provider: "p", Verdict: ports.ScanSafe})

	require.Error(t, err)
}

func TestSave_APurgedUrlIsNotFound(t *testing.T) {
	id := entity.IDToString(entity.NewID())
	q := &fakeQuerier{err: &pgconn.PgError{Code: "23503"}}

	err := newAdapter(q).Save(context.Background(), id, ports.ScanResult{Provider: "p", Verdict: ports.ScanSafe})

	require.True(t, errdefs.IsNotFound(err), "got %v", err)
}

func mustParse(t *testing.T, id string) [16]byte {
	t.Helper()
	parsed, err := entity.StringToID(id)
	require.NoError(t, err)
	return parsed
}
