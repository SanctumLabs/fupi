package urlrepo

import (
	"context"

	"github.com/sanctumlabs/curtz/app/internal/core/ports/repository"
	"github.com/sanctumlabs/curtz/app/internal/domain/url"
	"github.com/sanctumlabs/curtz/app/internal/pkg/common"
	"github.com/sanctumlabs/curtz/app/pkg/infra/database"
)

func NewUrlReadRepoAdapter(dbClient database.PostgresDatabaseClient) url.UrlReadRepository {
	repo := &urlReadRepositoryAdapter{
		dbClient:  dbClient,
		logPrefix: "UrlReadRepoAdapter"}

	return repo
}

func (repo *urlReadRepositoryAdapter) FetchById(ctx context.Context, id string) (url.URL, error) {
	panic("not implemented")
}

func (repo *urlReadRepositoryAdapter) FetchAll(ctx context.Context, params common.RequestParams) (repository.FetchRecordsResponse[url.URL], error) {
	panic("not implemented")
}

func (repo *urlReadRepositoryAdapter) FetchByShortCode(ctx context.Context, shortCode string) (url.URL, error) {
	panic("not implemented")
}

func (repo *urlReadRepositoryAdapter) FetchByCustomAlias(ctx context.Context, customAlias string) (url.URL, error) {
	panic("not implemented")
}

func (repo *urlReadRepositoryAdapter) FetchByUserId(ctx context.Context, userId string) (repository.FetchRecordsResponse[url.URL], error) {
	panic("not implemented")
}

func (repo *urlReadRepositoryAdapter) FetchByStatus(ctx context.Context, status url.URLStatus) (repository.FetchRecordsResponse[url.URL], error) {
	panic("not implemented")
}

func (repo *urlReadRepositoryAdapter) FetchByOriginalUrl(ctx context.Context, originalUrl string) (url.URL, error) {
	panic("not implemented")
}
