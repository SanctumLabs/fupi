package urlapi

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/sanctumlabs/fupi/app/internal/domain/url"
	"github.com/sanctumlabs/fupi/app/internal/pkg/common"
)

const (
	defaultPage = 1
	defaultSize = 100
	maxSize     = 100

	// maxPage keeps the offset (page-1)*size within the int32 the query takes.
	maxPage = math.MaxInt32 / maxSize
)

// listQueryError is a rejected query parameter. Its message names the parameter so the caller can fix the request.
type listQueryError struct {
	message string
}

func (e listQueryError) Error() string { return e.message }

// parseListParams reads the paging, ordering and filtering query parameters of GET /urls. Every value is checked
// against an allow-list: the SQL silently ignores an unknown order, which would hide a typo behind a plausible page.
// include_deleted is deliberately not read, so a caller cannot ask for their deleted urls.
func parseListParams(ctx *fiber.Ctx) (common.RequestParams, error) {
	page, err := intQuery(ctx, "page", defaultPage, 1, maxPage)
	if err != nil {
		return common.RequestParams{}, err
	}
	size, err := intQuery(ctx, "size", defaultSize, 1, maxSize)
	if err != nil {
		return common.RequestParams{}, err
	}

	opts := []common.RequestParamOptions{
		common.WithRequestLimit(size),
		common.WithOffset((page - 1) * size),
	}

	if raw := ctx.Query("order_by"); raw != "" {
		switch orderBy := common.OrderBy(raw); orderBy {
		case common.OrderByCreatedAt, common.OrderByUpdatedAt:
			opts = append(opts, common.WithOrderBy(orderBy))
		default:
			return common.RequestParams{}, listQueryError{"order_by must be one of: created_at, updated_at"}
		}
	}

	if raw := ctx.Query("sort_order"); raw != "" {
		switch sortOrder := common.SortOrder(strings.ToUpper(raw)); sortOrder {
		case common.SortOderAsc, common.SortOrderDesc:
			opts = append(opts, common.WithSortOrder(sortOrder))
		default:
			return common.RequestParams{}, listQueryError{"sort_order must be one of: ASC, DESC"}
		}
	}

	if raw := ctx.Query("status"); raw != "" {
		switch status := url.URLStatus(strings.ToUpper(raw)); status {
		case url.URLStatusActive, url.URLStatusExpired, url.URLStatusSuspended:
			opts = append(opts, common.WithStatus(string(status)))
		default:
			return common.RequestParams{}, listQueryError{"status must be one of: ACTIVE, EXPIRED, SUSPENDED"}
		}
	}

	return common.NewRequestParams(opts...), nil
}

// intQuery reads an integer query parameter within [min, max], or def when it is absent.
func intQuery(ctx *fiber.Ctx, name string, def, min, max int) (int, error) {
	raw := ctx.Query(name)
	if raw == "" {
		return def, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < min || value > max {
		return 0, listQueryError{fmt.Sprintf("%s must be an integer between %d and %d", name, min, max)}
	}
	return value, nil
}
