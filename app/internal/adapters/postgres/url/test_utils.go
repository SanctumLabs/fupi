package urldatastore

import (
	"context"

	postgresrepo "github.com/sanctumlabs/curtz/app/internal/adapters/postgres"
)

// injectMockUrlWriteTx wires a mockUrlWriteQuerier into the adapter, replacing the real DB
// transaction executor. The fn passed to withTx is called directly with the mock querier — no
// real connection or transaction is involved.
func injectMockUrlWriteTx(adapter *urlWriteDatastoreAdapter, q postgresrepo.UrlWriteQuerier) {
	adapter.withTx = func(ctx context.Context, fn func(postgresrepo.UrlWriteQuerier) error) error {
		return fn(q)
	}
}
