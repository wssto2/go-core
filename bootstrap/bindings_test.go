package bootstrap

import (
	"log/slog"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/audit"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/event"
	"github.com/wssto2/go-core/health"
	"github.com/wssto2/go-core/observability"
)

func TestDefaultInfrastructureBindsCoreServices(t *testing.T) {
	cfg := DefaultConfig()
	builder := New(cfg)
	builder.DefaultInfrastructure()
	c := builder.container
	c.EnableStrictMode()

	// Check core services that are always bound
	require.NotPanics(t, func() {
		_ = MustResolve[*gin.Engine](c)
		_ = MustResolve[*slog.Logger](c)
		_ = MustResolve[*observability.Telemetry](c)
		_ = MustResolve[event.Bus](c)
		_ = MustResolve[*health.HealthRegistry](c)
	})

	// Database.Registry and audit.Repository are only bound if databases are configured
	// In strict mode, missing services panic, so check with non-strict mode
	c.strict = false
	_, err := Resolve[*database.Registry](c)
	require.Error(t, err)
	_, err = Resolve[audit.Repository](c)
	require.Error(t, err)
}
