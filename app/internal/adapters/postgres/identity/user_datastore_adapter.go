package identitydatastore

import (
	"github.com/google/wire"
	postgresrepo "github.com/sanctumlabs/curtz/app/internal/adapters/postgres"
	"github.com/sanctumlabs/curtz/app/internal/domain/identity"
	"github.com/sanctumlabs/curtz/app/pkg/infra/database"
)

type (
	userWriteDatastoreAdapter struct {
		logPrefix string
		dbClient  database.PostgresDatabaseClient
		config    database.Config
		withTx    postgresrepo.TxExecutor[postgresrepo.UserWriteQuerier]
	}

	userReadDatastoreAdapter struct {
		logPrefix string
		dbClient  database.PostgresDatabaseClient
		config    database.Config
		withTx    postgresrepo.TxExecutor[postgresrepo.UserReadQuerier]
	}

	userDatastoreAdapter struct {
		userReadDatastoreAdapter
		userWriteDatastoreAdapter
	}
)

var (
	_ identity.UserDatastore      = (*userDatastoreAdapter)(nil)
	_ identity.UserWriteDatastore = (*userWriteDatastoreAdapter)(nil)
	_ identity.UserReadDatastore  = (*userReadDatastoreAdapter)(nil)

	UserWriteDatastoreAdapter = wire.NewSet(NewUserWriteDatastoreAdapter)
	UserReadDatastoreAdapter  = wire.NewSet(NewUserReadDatastoreAdapter)
	UserDatastoreAdapter      = wire.NewSet(NewUserDatastoreAdapter)
)

func NewUserDatastoreAdapter(dbClient database.PostgresDatabaseClient, config database.Config) identity.UserDatastore {
	repo := &userDatastoreAdapter{
		userReadDatastoreAdapter:  *NewUserReadDatastoreAdapter(dbClient, config).(*userReadDatastoreAdapter),
		userWriteDatastoreAdapter: *NewUserWriteDatastoreAdapter(dbClient, config).(*userWriteDatastoreAdapter),
	}

	return repo
}
