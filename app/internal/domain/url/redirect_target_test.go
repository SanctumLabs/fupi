package url

import (
	"testing"
	"time"

	"github.com/sanctumlabs/fupi/app/pkg/errdefs"
	"github.com/stretchr/testify/require"
)

func TestRedirectTarget_OnlyAnActiveUnexpiredTargetRedirects(t *testing.T) {
	future, past := time.Now().Add(time.Hour), time.Now().Add(-time.Hour)
	cases := map[string]struct {
		status    URLStatus
		expiresOn time.Time
		wantErr   error
	}{
		"active":    {URLStatusActive, future, nil},
		"expired":   {URLStatusActive, past, errdefs.ErrURLExpired},
		"suspended": {URLStatusSuspended, future, errdefs.ErrURLNotActive},
		"deleted":   {URLStatusDeleted, future, errdefs.ErrURLNotActive},
		"marked":    {URLStatusExpired, future, errdefs.ErrURLNotActive},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			target, err := NewRedirectTarget("https://example.com/target/page", tc.status, tc.expiresOn)
			require.NoError(t, err)

			got, err := target.Redirect()

			require.ErrorIs(t, err, tc.wantErr)
			if tc.wantErr == nil {
				require.Equal(t, "https://example.com/target/page", got.Value())
			}
		})
	}
}

func TestNewRedirectTarget_RejectsAnInvalidUrl(t *testing.T) {
	_, err := NewRedirectTarget("not a url", URLStatusActive, time.Now())
	require.Error(t, err)
}
