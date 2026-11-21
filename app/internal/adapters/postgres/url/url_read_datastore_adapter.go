package urldatastore

import (
	"context"
	"time"

	"github.com/sanctumlabs/curtz/app/internal/core/ports/repository"
	"github.com/sanctumlabs/curtz/app/internal/domain/url"
	"github.com/sanctumlabs/curtz/app/internal/pkg/common"
	"github.com/sanctumlabs/curtz/app/pkg/infra/database"
)

func NewUrlReadDatastoreAdapter(dbClient database.PostgresDatabaseClient) url.UrlReadDatastore {
	repo := &urlReadDatastoreAdapter{
		dbClient:  dbClient,
		logPrefix: "UrlReadDatastoreAdapter"}

	return repo
}

func (repo *urlReadDatastoreAdapter) FetchById(ctx context.Context, id string) (url.URL, error) {
	panic("not implemented")
}

func (repo *urlReadDatastoreAdapter) FetchAll(ctx context.Context, params common.RequestParams) (repository.FetchRecordsResponse[url.URL], error) {
	panic("not implemented")
}

func (repo *urlReadDatastoreAdapter) FetchByShortCode(ctx context.Context, shortCode string) (url.URL, error) {
	panic("not implemented")
}

func (repo *urlReadDatastoreAdapter) FetchByCustomAlias(ctx context.Context, customAlias string) (url.URL, error) {
	panic("not implemented")
}

func (repo *urlReadDatastoreAdapter) FetchByUserId(ctx context.Context, userId string) (repository.FetchRecordsResponse[url.URL], error) {
	panic("not implemented")
}

func (repo *urlReadDatastoreAdapter) FetchByStatus(ctx context.Context, status url.URLStatus) (repository.FetchRecordsResponse[url.URL], error) {
	panic("not implemented")
}

func (repo *urlReadDatastoreAdapter) FetchByOriginalUrl(ctx context.Context, originalUrl string) (url.URL, error) {
	panic("not implemented")
}

func (repo *urlReadDatastoreAdapter) FetchExpiredActive(ctx context.Context, before time.Time, limit int) ([]url.URL, error) {
	panic("not implemented")
}
