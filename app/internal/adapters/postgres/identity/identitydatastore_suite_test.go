package identitydatastore

import (
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
)

// TestIdentityDatastore is the single Ginkgo entrypoint for this package. Every ginkgo.Describe
// in the package — unit, and integration when built with `-tags integration` — runs under it.
func TestIdentityDatastore(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Identity Datastore Suite")
}
