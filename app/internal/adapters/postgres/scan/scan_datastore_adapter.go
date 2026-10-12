// Package scandatastore is the Postgres implementation of ports.UrlScanDatastore: the stored results of url scans.
package scandatastore

import (
	"context"
	"fmt"
	"time"

	postgresql "github.com/sanctumlabs/fupi/app/internal/adapters/postgres/sql"
	"github.com/sanctumlabs/fupi/app/internal/ports"
	"github.com/sanctumlabs/fupi/app/pkg/infra/database"
	"github.com/sanctumlabs/fupi/app/pkg/infra/database/postgres"
)

var _ ports.UrlScanDatastore = (*Adapter)(nil)

// querier is the part of the generated queries the adapter uses.
type querier interface {
	QueryUrlScanExistsByUrlIdAndProvider(ctx context.Context, arg postgresql.QueryUrlScanExistsByUrlIdAndProviderParams) (bool, error)
	QueryCreateUrlScan(ctx context.Context, arg postgresql.QueryCreateUrlScanParams) (postgresql.UrlScan, error)
}

// Adapter implements ports.UrlScanDatastore on Postgres.
type Adapter struct {
	queries querier
	timeout time.Duration
}

// NewAdapter builds the adapter. timeout bounds every statement.
func NewAdapter(client database.PostgresDatabaseClient, timeout time.Duration) *Adapter {
	return &Adapter{queries: postgresql.New(client.GetDB()), timeout: timeout}
}

// ExistsByUrlAndProvider reports whether the provider already scanned the url.
func (a *Adapter) ExistsByUrlAndProvider(ctx context.Context, urlID, provider string) (bool, error) {
	id, err := postgres.StringToUUID(urlID)
	if err != nil {
		return false, err
	}
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	exists, err := a.queries.QueryUrlScanExistsByUrlIdAndProvider(ctx, postgresql.QueryUrlScanExistsByUrlIdAndProviderParams{UrlID: id, Provider: provider})
	if err != nil {
		return false, fmt.Errorf("check for a scan of url %s by %s: %w", urlID, provider, err)
	}
	return exists, nil
}

// Save stores the result of scanning the url.
func (a *Adapter) Save(ctx context.Context, urlID string, result ports.ScanResult) error {
	id, err := postgres.StringToUUID(urlID)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	if _, err := a.queries.QueryCreateUrlScan(ctx, postgresql.QueryCreateUrlScanParams{
		UrlID:       id,
		Provider:    result.Provider,
		Result:      string(result.Verdict),
		RawResponse: result.RawResponse,
	}); err != nil {
		return fmt.Errorf("save the scan of url %s by %s: %w", urlID, result.Provider, err)
	}
	return nil
}
