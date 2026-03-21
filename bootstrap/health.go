package bootstrap

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/utils"
	"gorm.io/gorm"
)

type HealthChecker interface {
	Name() string
	Check(ctx context.Context) error
}

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

// HealthHandler returns a standard health check endpoint.

func HealthHandler(checkers ...HealthChecker) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		results := map[string]string{}
		status := http.StatusOK
		for _, c := range checkers {
			if err := c.Check(ctx.Request.Context()); err != nil {
				results[c.Name()] = "down: " + err.Error()
				status = http.StatusServiceUnavailable
			} else {
				results[c.Name()] = "up"
			}
		}
		ctx.JSON(status, gin.H{"status": utils.IfThenElse(status == 200, "up", "degraded"), "checks": results})
	}
}
