//go:build integration

package postgresrepo_test

import (
	"context"
	"testing"

	postgresrepo "github.com/sanctumlabs/fupi/app/internal/adapters/postgres"
	"github.com/sanctumlabs/fupi/app/internal/ports"
	"github.com/sanctumlabs/fupi/app/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Two generators stand in for two API instances drawing from the same short_code_counter: the blocks they claim must
// never overlap, so no code is issued twice. 1500 codes each crosses a block boundary.
func TestSequenceShortCodeGenerator_InstancesNeverIssueTheSameCode(t *testing.T) {
	ctx := context.Background()
	dbClient := test.TestPostgresDatabaseClientHelper(t, ctx)
	t.Cleanup(dbClient.Close)

	generators := []ports.ShortCodeGenerator{
		postgresrepo.NewSequenceShortCodeGenerator(dbClient),
		postgresrepo.NewSequenceShortCodeGenerator(dbClient),
	}

	seen := map[string]struct{}{}
	for range 1500 {
		for _, generator := range generators {
			code, err := generator.Next(ctx)
			require.NoError(t, err)
			_, dup := seen[code.Value()]
			require.False(t, dup, "code %s was issued twice", code.Value())
			seen[code.Value()] = struct{}{}
		}
	}
	assert.Len(t, seen, 3000)
}
