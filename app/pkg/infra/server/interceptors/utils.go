package interceptors

import "strings"

func extractMethodName(fullMethod string) string {
	parts := strings.Split(fullMethod, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return fullMethod
}
