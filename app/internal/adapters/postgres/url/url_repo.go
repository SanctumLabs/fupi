package urlrepo

import (
	"context"

	"github.com/google/wire"
	postgresrepo "github.com/sanctumlabs/curtz/app/internal/adapters/postgres"
	"github.com/sanctumlabs/curtz/app/internal/domain/identity"
	"github.com/sanctumlabs/curtz/app/internal/domain/url"
	"github.com/sanctumlabs/curtz/app/pkg/infra/database"
)

type (
	urlWriteRepositoryAdapter struct {
		logPrefix string
		dbClient  database.PostgresDatabaseClient
		config    database.Config
		userRepo  identity.UserReadRepository
		// withTx is the transaction executor. In production it wraps postgres.WithTransaction;
		// in tests it can be replaced with a function that calls the mock querier directly,
		// bypassing the real database entirely.
		withTx func(ctx context.Context, fn func(q postgresrepo.UrlWriteQuerier) (url.URL, error)) (url.URL, error)
	}

	urlReadRepositoryAdapter struct {
		logPrefix string
		dbClient  database.PostgresDatabaseClient
	}

	urlRepoAdapter struct {
		urlReadRepositoryAdapter
		urlWriteRepositoryAdapter
	}
)

var (
	_ url.UrlRepository      = (*urlRepoAdapter)(nil)
	_ url.UrlWriteRepository = (*urlWriteRepositoryAdapter)(nil)
	_ url.UrlReadRepository  = (*urlReadRepositoryAdapter)(nil)

	UrlWriteRepoAdapter = wire.NewSet(NewUrlWriteRepoAdapter)
	UrlReadRepoAdapter  = wire.NewSet(NewUrlReadRepoAdapter)
	UrlRepoAdapter      = wire.NewSet(NewUrlRepoAdapter)
)

func NewUrlRepoAdapter(dbClient database.PostgresDatabaseClient, userRepo identity.UserReadRepository, config database.Config) url.UrlRepository {
	repo := &urlRepoAdapter{
		urlReadRepositoryAdapter:  *NewUrlReadRepoAdapter(dbClient).(*urlReadRepositoryAdapter),
		urlWriteRepositoryAdapter: *NewUrlWriteRepoAdapter(dbClient, userRepo, config).(*urlWriteRepositoryAdapter),
	}

	return repo
}
