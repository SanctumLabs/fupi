package identitydatastore

import (
	"context"

	postgresrepo "github.com/sanctumlabs/curtz/app/internal/adapters/postgres"
)

// injectMockUserReadTx wires a mockUserReadQuerier into the adapter, replacing the real DB
// transaction executor. The fn passed to withTx is called directly with
// the mock querier — no real connection or transaction is involved.
func injectMockUserReadTx(adapter *userReadDatastoreAdapter, q postgresrepo.UserReadQuerier) {
	adapter.withTx = func(ctx context.Context, fn func(postgresrepo.UserReadQuerier) error) error {
		return fn(q)
	}
}

// injectMockUserWriteTx wires a mockUserWriteQuerier into the adapter, replacing the real DB
// transaction executor. The fn passed to withTx is called directly with
// the mock querier — no real connection or transaction is involved.
func injectMockUserWriteTx(adapter *userWriteDatastoreAdapter, q postgresrepo.UserWriteQuerier) {
	adapter.withTx = func(ctx context.Context, fn func(postgresrepo.UserWriteQuerier) error) error {
		return fn(q)
	}
}
