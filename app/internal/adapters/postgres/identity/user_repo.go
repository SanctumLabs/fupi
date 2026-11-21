package identityrepo

import (
	"context"

	"github.com/google/wire"
	postgresrepo "github.com/sanctumlabs/curtz/app/internal/adapters/postgres"
	"github.com/sanctumlabs/curtz/app/internal/domain/identity"
	"github.com/sanctumlabs/curtz/app/pkg/infra/database"
)

type (
	userWriteRepositoryAdapter struct {
		logPrefix string
		dbClient  database.PostgresDatabaseClient
		config    database.Config
		// withTx is the transaction executor. In production it wraps postgres.WithTransaction;
		// in tests it can be replaced with a function that calls the mock querier directly,
		// bypassing the real database entirely.
		withTx func(ctx context.Context, fn func(q postgresrepo.UserWriteQuerier) (identity.User, error)) (identity.User, error)
	}

	userReadRepositoryAdapter struct {
		logPrefix string
		dbClient  database.PostgresDatabaseClient
		config    database.Config
		withTx    func(ctx context.Context, fn func(q postgresrepo.UserReadQuerier) (identity.User, error)) (identity.User, error)
	}

	userRepoAdapter struct {
		userReadRepositoryAdapter
		userWriteRepositoryAdapter
	}
)

var (
	_ identity.UserRepository      = (*userRepoAdapter)(nil)
	_ identity.UserWriteRepository = (*userWriteRepositoryAdapter)(nil)
	_ identity.UserReadRepository  = (*userReadRepositoryAdapter)(nil)

	UserWriteRepoAdapter = wire.NewSet(NewUserWriteRepoAdapter)
	UserReadRepoAdapter  = wire.NewSet(NewUserReadRepoAdapter)
	UserRepoAdapter      = wire.NewSet(NewUserRepoAdapter)
)

func NewUserRepoAdapter(dbClient database.PostgresDatabaseClient, config database.Config) identity.UserRepository {
	repo := &userRepoAdapter{
		userReadRepositoryAdapter:  *NewUserReadRepoAdapter(dbClient, config).(*userReadRepositoryAdapter),
		userWriteRepositoryAdapter: *NewUserWriteRepoAdapter(dbClient, config).(*userWriteRepositoryAdapter),
	}

	return repo
}
