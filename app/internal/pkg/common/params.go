package common

import "time"

// RequestParams are parameters that can be passed when fetching records from a database
type RequestParams struct {
	// IncludedDeleted includes "softly" deleted records in the result of the query
	IncludeDeleted bool

	// IsActive includes active records in the result of the query
	IsActive bool

	// Limit is the page size
	Limit int

	// Offset is the page number or cursor
	Offset int

	// OrderOption contains ordering options for the filter
	OrderOption RequestParamsOrderOption

	// DateRangeOption contains date range options to filter records by
	DateRangeOption RequestParamsDateRangeOption
}

// RequestParamOptions is a functional option that allows setting values to request parameters
type RequestParamOptions func(*RequestParams)

// WithRequestLimit sets the limit of a request
func WithRequestLimit(limit int) RequestParamOptions {
	return func(rp *RequestParams) {
		rp.Limit = limit
	}
}

// WithOffset sets the offset of a pagination request
func WithOffset(offset int) RequestParamOptions {
	return func(rp *RequestParams) {
		rp.Offset = offset
	}
}

// WithIncludeDeleted sets whether to include deleted records
func WithIncludeDeleted(hasDeleted bool) RequestParamOptions {
	return func(rp *RequestParams) {
		rp.IncludeDeleted = hasDeleted
	}
}

// WithSortOrder sets the sorting order
func WithSortOrder(sortOrder SortOrder) RequestParamOptions {
	return func(rp *RequestParams) {
		rp.OrderOption.SortOrder = sortOrder
	}
}

// WithOrderBy sets what field to order by
func WithOrderBy(orderBy OrderBy) RequestParamOptions {
	return func(rp *RequestParams) {
		rp.OrderOption.OrderBy = orderBy
	}
}

// WithAmountSortOrder sets the sorting order for the amount field
func WithAmountSortOrder(sortOrder SortOrder) RequestParamOptions {
	return func(rp *RequestParams) {
		rp.OrderOption.AmountSortOrder = sortOrder
	}
}

// WithDateFilter sets the date filter option
func WithDateFilter(dateFilter DateField) RequestParamOptions {
	return func(rp *RequestParams) {
		rp.DateRangeOption.DateFieldOption = dateFilter
	}
}

// WithStartDate sets the start date for the date range
func WithStartDate(startDate time.Time) RequestParamOptions {
	return func(rp *RequestParams) {
		rp.DateRangeOption.StartDate = startDate
	}
}

// WithEndDate sets the end date for the date range
func WithEndDate(endDate time.Time) RequestParamOptions {
	return func(rp *RequestParams) {
		rp.DateRangeOption.EndDate = endDate
	}
}

// NewRequestParams creates a new request params object with reasonable defaults set
func NewRequestParams(opts ...RequestParamOptions) RequestParams {
	r := RequestParams{
		Limit:          100,
		Offset:         0,
		IncludeDeleted: false,
		OrderOption: RequestParamsOrderOption{
			OrderBy:         OrderByCreatedAt,
			SortOrder:       SortOrderDesc,
			AmountSortOrder: SortOrderDesc,
		},
	}

	for _, opt := range opts {
		opt(&r)
	}

	return r
}
