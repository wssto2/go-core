package storetest_test

import (
	"testing"

	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authztest"
	"github.com/wssto2/go-core/authz/storetest"
)

// The in-memory store used by application tests must itself conform.
func TestMemoryStoreConforms(t *testing.T) {
	storetest.Run(t, func(*testing.T) authz.Store { return authztest.NewMemoryStore() })
}
