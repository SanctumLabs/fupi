//go:build integration

package scandatastore_test

import (
	"context"
	"testing"
	"time"

	scandatastore "github.com/sanctumlabs/fupi/app/internal/adapters/postgres/scan"
	"github.com/sanctumlabs/fupi/app/internal/core/entity"
	"github.com/sanctumlabs/fupi/app/internal/ports"
	"github.com/sanctumlabs/fupi/app/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaveAndExists_AgainstPostgres(t *testing.T) {
	ctx := context.Background()
	client := test.TestPostgresDatabaseClientHelper(t, ctx)
	t.Cleanup(client.Close)
	adapter := scandatastore.NewAdapter(client, 10*time.Second)

	userID := entity.IDToString(entity.NewID())
	urlID := entity.IDToString(entity.NewID())
	_, err := client.GetDB().Exec(ctx, `INSERT INTO users (id, username, email, password_hash) VALUES ($1, 'scanuser', 'scan@fupi.com', 'x')`, userID)
	require.NoError(t, err)
	_, err = client.GetDB().Exec(ctx, `INSERT INTO urls (id, user_id, short_code, original_url, expires_on) VALUES ($1, $2, 'scan01', 'https://example.com/', NOW() + INTERVAL '1 day')`, urlID, userID)
	require.NoError(t, err)

	exists, err := adapter.ExistsByUrlAndProvider(ctx, urlID, "google_safe_browsing")
	require.NoError(t, err)
	assert.False(t, exists)

	require.NoError(t, adapter.Save(ctx, urlID, ports.ScanResult{
		Provider: "google_safe_browsing", Verdict: ports.ScanPhishing, RawResponse: []byte(`{"matches":[{"threatType":"SOCIAL_ENGINEERING"}]}`),
	}))

	exists, err = adapter.ExistsByUrlAndProvider(ctx, urlID, "google_safe_browsing")
	require.NoError(t, err)
	assert.True(t, exists)
	exists, err = adapter.ExistsByUrlAndProvider(ctx, urlID, "virustotal")
	require.NoError(t, err)
	assert.False(t, exists, "another provider has not scanned it")
}
