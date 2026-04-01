package health

import (
	"context"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// HealthChecker is the interface for all components that can report their status.
type HealthChecker interface {
	Name() string
	Check(ctx context.Context) error
}

// HealthRegistry manages a list of health checkers.
type HealthRegistry struct {
	mu       sync.RWMutex
	checkers []HealthChecker
	draining bool
}

// NewHealthRegistry creates a new HealthRegistry.
func NewHealthRegistry() *HealthRegistry {
	return &HealthRegistry{}
}

// SetDraining toggles the registry draining state. When draining, readiness will return 503.
func (r *HealthRegistry) SetDraining(v bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.draining = v
}

func (r *HealthRegistry) IsDraining() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.draining
}

// Add adds one or more health checkers to the registry.
func (r *HealthRegistry) Add(checkers ...HealthChecker) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.checkers = append(r.checkers, checkers...)
}

// Checkers returns all registered health checkers.
func (r *HealthRegistry) Checkers() []HealthChecker {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.checkers
}

// DBHealthChecker checks the database connectivity.
type DBHealthChecker struct{ db *gorm.DB }

func NewDBHealthChecker(db *gorm.DB) HealthChecker {
	return &DBHealthChecker{db: db}
}

func (c *DBHealthChecker) Name() string {
	return "database"
}

func (c *DBHealthChecker) Check(ctx context.Context) error {
	sqlDB, err := c.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}

// Pinger is an optional interface that event bus implementations may
// satisfy to support health checking.
type Pinger interface {
	Ping(ctx context.Context) error
}

// EventBusChecker checks event bus connectivity. It expects the bus to
// implement the Pinger interface. If it does not, Check always returns nil
// (no-op — the bus does not support health checks).
type EventBusChecker struct {
	bus any // stored as any to avoid importing event package
}

// NewEventBusChecker wraps a bus for health checking.
// If bus implements Pinger, its Ping method is called on each check.
func NewEventBusChecker(bus any) *EventBusChecker {
	return &EventBusChecker{bus: bus}
}

// Check calls Ping on the bus if it implements Pinger.
func (e *EventBusChecker) Name() string {
	return "eventbus"
}
func (e *EventBusChecker) Check(ctx context.Context) error {
	if p, ok := e.bus.(Pinger); ok {
		return p.Ping(ctx)
	}
	return nil // bus does not support Ping — health check is a no-op
}

// LivenessHandler returns a simple OK response indicating the process is running.
func LivenessHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "up"})
	}
}

// ReadinessHandler runs all registered checks and returns their status.
func ReadinessHandler(registry *HealthRegistry) gin.HandlerFunc {
	return func(c *gin.Context) {
		// If we're draining (zero-downtime deploy), immediately report not ready.
		if registry.IsDraining() {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "draining",
				"checks": gin.H{"drain": "draining"},
			})
			return
		}

		checkers := registry.Checkers()
		results := map[string]string{}
		status := http.StatusOK

		for _, checker := range checkers {
			if err := checker.Check(c.Request.Context()); err != nil {
				results[checker.Name()] = "down: " + err.Error()
				status = http.StatusServiceUnavailable
			} else {
				results[checker.Name()] = "up"
			}
		}

		statusText := "up"
		if status != http.StatusOK {
			statusText = "degraded"
		}

		c.JSON(status, gin.H{
			"status": statusText,
			"checks": results,
		})
	}
}
