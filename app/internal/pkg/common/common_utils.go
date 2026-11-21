package common

import (
	"fmt"
	"strings"
)

func GetDateRangeDescription(filter RequestParamsDateRangeOption) string {
	if filter.StartDate.IsZero() && filter.EndDate.IsZero() {
		return "No date filter"
	}

	var desc strings.Builder
	desc.WriteString("Date filter: ")

	if !filter.StartDate.IsZero() {
		desc.WriteString(fmt.Sprintf("from %s", filter.StartDate.Format("2006-01-02")))
	}

	if !filter.EndDate.IsZero() {
		if !filter.StartDate.IsZero() {
			desc.WriteString(" ")
		}
		desc.WriteString(fmt.Sprintf("to %s", filter.EndDate.Format("2006-01-02")))
	}

	return desc.String()
}
