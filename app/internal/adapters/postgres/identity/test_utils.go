package identityrepo

import (
	"context"

	postgresrepo "github.com/sanctumlabs/curtz/app/internal/adapters/postgres"
	"github.com/sanctumlabs/curtz/app/internal/domain/identity"
)

// injectMockUserReadTx wires a mockUserReadQuerier into the adapter, replacing the real DB
// transaction executor. The fn passed to withTx is called directly with
// the mock querier — no real connection or transaction is involved.
func injectMockUserReadTx(adapter *userReadRepositoryAdapter, q postgresrepo.UserReadQuerier) {
	adapter.withTx = func(ctx context.Context, fn func(postgresrepo.UserReadQuerier) (identity.User, error)) (identity.User, error) {
		return fn(q)
	}
}

// injectMockUserWriteTx wires a mockUserWriteQuerier into the adapter, replacing the real DB
// transaction executor. The fn passed to withTx is called directly with
// the mock querier — no real connection or transaction is involved.
func injectMockUserWriteTx(adapter *userWriteRepositoryAdapter, q postgresrepo.UserWriteQuerier) {
	adapter.withTx = func(ctx context.Context, fn func(postgresrepo.UserWriteQuerier) (identity.User, error)) (identity.User, error) {
		return fn(q)
	}
}
