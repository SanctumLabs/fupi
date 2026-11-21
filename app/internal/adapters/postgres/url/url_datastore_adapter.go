package urldatastore

import (
	"github.com/google/wire"
	postgresrepo "github.com/sanctumlabs/curtz/app/internal/adapters/postgres"
	"github.com/sanctumlabs/curtz/app/internal/domain/url"
	"github.com/sanctumlabs/curtz/app/pkg/infra/database"
)

type (
	urlWriteDatastoreAdapter struct {
		logPrefix string
		dbClient  database.PostgresDatabaseClient
		config    database.Config
		withTx    postgresrepo.TxExecutor[postgresrepo.UrlWriteQuerier]
	}

	urlReadDatastoreAdapter struct {
		logPrefix string
		dbClient  database.PostgresDatabaseClient
	}

	urlDatastoreAdapter struct {
		urlReadDatastoreAdapter
		urlWriteDatastoreAdapter
	}
)

var (
	_ url.UrlDatastore      = (*urlDatastoreAdapter)(nil)
	_ url.UrlWriteDatastore = (*urlWriteDatastoreAdapter)(nil)
	_ url.UrlReadDatastore  = (*urlReadDatastoreAdapter)(nil)

	UrlWriteDatastoreAdapter = wire.NewSet(NewUrlWriteDatastoreAdapter)
	UrlReadDatastoreAdapter  = wire.NewSet(NewUrlReadDatastoreAdapter)
	UrlDatastoreAdapter      = wire.NewSet(NewUrlDatastoreAdapter)
)

func NewUrlDatastoreAdapter(dbClient database.PostgresDatabaseClient, config database.Config) url.UrlDatastore {
	return &urlDatastoreAdapter{
		urlReadDatastoreAdapter:  *NewUrlReadDatastoreAdapter(dbClient).(*urlReadDatastoreAdapter),
		urlWriteDatastoreAdapter: *NewUrlWriteDatastoreAdapter(dbClient, config).(*urlWriteDatastoreAdapter),
	}
}
