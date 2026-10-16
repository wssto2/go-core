package identitytest_test

import (
	"testing"

	"github.com/wssto2/go-core/identity/identitytest"
	"github.com/wssto2/go-core/identity/storetest"
)

// The memory stores pass the same suite as the SQL ones.
func TestMemoryStoresConform(t *testing.T) {
	storetest.Run(t, func(*testing.T) storetest.Stores {
		changes, sessions := identitytest.NewChangeLog(identitytest.NewClock(identitytest.Epoch)), identitytest.NewSessions()

		signIns := identitytest.NewSignIns()

		return storetest.Stores{
			Accounts: identitytest.NewAccounts().WithSignIns(signIns), SignIns: signIns, Sessions: sessions,
			Codes: identitytest.NewCodes(), Reauth: identitytest.NewReauth(),
			Changes: changes, Activity: identitytest.NewActivityLog(changes, sessions),
		}
	})
}
