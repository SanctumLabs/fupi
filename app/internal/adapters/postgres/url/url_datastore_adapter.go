package urldatastore

import (
	"github.com/google/wire"
	postgresrepo "github.com/sanctumlabs/fupi/app/internal/adapters/postgres"
	"github.com/sanctumlabs/fupi/app/internal/domain/url"
	"github.com/sanctumlabs/fupi/app/pkg/infra/database"
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
		config    database.Config
		withTx    postgresrepo.TxExecutor[postgresrepo.UrlReadQuerier]
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
		urlReadDatastoreAdapter:  *NewUrlReadDatastoreAdapter(dbClient, config).(*urlReadDatastoreAdapter),
		urlWriteDatastoreAdapter: *NewUrlWriteDatastoreAdapter(dbClient, config).(*urlWriteDatastoreAdapter),
	}
}
