package identitytest_test

import (
	"testing"

	"github.com/wssto2/go-core/identity/identitytest"
	"github.com/wssto2/go-core/identity/storetest"
)

// The memory stores pass the same suite as the SQL ones.
func TestMemoryStoresConform(t *testing.T) {
	storetest.Run(t, func(*testing.T) storetest.Stores {
		return storetest.Stores{Accounts: identitytest.NewAccounts(), SignIns: identitytest.NewSignIns(), Sessions: identitytest.NewSessions()}
	})
}
