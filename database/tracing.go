package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/wssto2/go-core/observability/tracing"
	"gorm.io/gorm"
)

const dbTraceFinishKey = "dbtrace_finish"

// EnableDBTracing registers GORM callbacks to start and finish tracing spans
// for common DB operation types (query, create, update, delete, row, raw).
// It is intentionally minimal: it stores the finish func in the current GORM
// statement instance and calls it in the corresponding after-callback.
func EnableDBTracing(conn *gorm.DB, tr tracing.Tracer) error {
	if conn == nil {
		return errors.New("conn is nil")
	}

	if tr == nil {
		return errors.New("tr is nil")
	}

	// Query callbacks
	{
		var err error

		cb := conn.Callback().Query()
		err = cb.Before("gorm:query").Register("dbtrace:before_query", func(db *gorm.DB) {
			ctx := db.Statement.Context
			if ctx == nil {
				ctx = context.Background()
			}
			ctx2, finish := tr.StartSpan(ctx, "db.query")
			db.Statement.Context = ctx2
			db.InstanceSet(dbTraceFinishKey, finish)
		})
		if err != nil {
			return fmt.Errorf("failed to register dbtrace:before_query: %w", err)
		}

		err = cb.After("gorm:after_query").Register("dbtrace:after_query", func(db *gorm.DB) {
			if v, ok := db.InstanceGet(dbTraceFinishKey); ok {
				if finish, ok := v.(func(error)); ok {
					finish(db.Error)
				}
			}
		})
		if err != nil {
			return fmt.Errorf("failed to register dbtrace:after_query: %w", err)
		}
	}

	// Create callbacks
	{
		var err error
		cb := conn.Callback().Create()
		err = cb.Before("gorm:create").Register("dbtrace:before_create", func(db *gorm.DB) {
			ctx := db.Statement.Context
			if ctx == nil {
				ctx = context.Background()
			}
			ctx2, finish := tr.StartSpan(ctx, "db.create")
			db.Statement.Context = ctx2
			db.InstanceSet(dbTraceFinishKey, finish)
		})
		if err != nil {
			return fmt.Errorf("failed to register dbtrace:before_create: %w", err)
		}

		err = cb.After("gorm:after_create").Register("dbtrace:after_create", func(db *gorm.DB) {
			if v, ok := db.InstanceGet(dbTraceFinishKey); ok {
				if finish, ok := v.(func(error)); ok {
					finish(db.Error)
				}
			}
		})
		if err != nil {
			return fmt.Errorf("failed to register dbtrace:after_create: %w", err)
		}
	}

	// Update callbacks
	{
		var err error
		cb := conn.Callback().Update()
		err = cb.Before("gorm:update").Register("dbtrace:before_update", func(db *gorm.DB) {
			ctx := db.Statement.Context
			if ctx == nil {
				ctx = context.Background()
			}
			ctx2, finish := tr.StartSpan(ctx, "db.update")
			db.Statement.Context = ctx2
			db.InstanceSet(dbTraceFinishKey, finish)
		})
		if err != nil {
			return fmt.Errorf("failed to register dbtrace:before_update: %w", err)
		}

		err = cb.After("gorm:after_update").Register("dbtrace:after_update", func(db *gorm.DB) {
			if v, ok := db.InstanceGet(dbTraceFinishKey); ok {
				if finish, ok := v.(func(error)); ok {
					finish(db.Error)
				}
			}
		})
		if err != nil {
			return fmt.Errorf("failed to register dbtrace:after_update: %w", err)
		}
	}

	// Delete callbacks
	{
		var err error
		cb := conn.Callback().Delete()
		err = cb.Before("gorm:delete").Register("dbtrace:before_delete", func(db *gorm.DB) {
			ctx := db.Statement.Context
			if ctx == nil {
				ctx = context.Background()
			}
			ctx2, finish := tr.StartSpan(ctx, "db.delete")
			db.Statement.Context = ctx2
			db.InstanceSet(dbTraceFinishKey, finish)
		})
		if err != nil {
			return fmt.Errorf("failed to register dbtrace:before_delete: %w", err)
		}

		err = cb.After("gorm:after_delete").Register("dbtrace:after_delete", func(db *gorm.DB) {
			if v, ok := db.InstanceGet(dbTraceFinishKey); ok {
				if finish, ok := v.(func(error)); ok {
					finish(db.Error)
				}
			}
		})
		if err != nil {
			return fmt.Errorf("failed to register dbtrace:after_delete: %w", err)
		}
	}

	// Row callbacks
	{
		var err error
		cb := conn.Callback().Row()
		err = cb.Before("gorm:row").Register("dbtrace:before_row", func(db *gorm.DB) {
			ctx := db.Statement.Context
			if ctx == nil {
				ctx = context.Background()
			}
			ctx2, finish := tr.StartSpan(ctx, "db.row")
			db.Statement.Context = ctx2
			db.InstanceSet(dbTraceFinishKey, finish)
		})
		if err != nil {
			return fmt.Errorf("failed to register dbtrace:before_row: %w", err)
		}

		err = cb.After("gorm:after_row").Register("dbtrace:after_row", func(db *gorm.DB) {
			if v, ok := db.InstanceGet(dbTraceFinishKey); ok {
				if finish, ok := v.(func(error)); ok {
					finish(db.Error)
				}
			}
		})
		if err != nil {
			return fmt.Errorf("failed to register dbtrace:after_row: %w", err)
		}
	}

	// Raw callbacks
	{
		var err error
		cb := conn.Callback().Raw()
		err = cb.Before("gorm:raw").Register("dbtrace:before_raw", func(db *gorm.DB) {
			ctx := db.Statement.Context
			if ctx == nil {
				ctx = context.Background()
			}
			ctx2, finish := tr.StartSpan(ctx, "db.raw")
			db.Statement.Context = ctx2
			db.InstanceSet(dbTraceFinishKey, finish)
		})
		if err != nil {
			return fmt.Errorf("failed to register dbtrace:before_raw: %w", err)
		}

		err = cb.After("gorm:after_raw").Register("dbtrace:after_raw", func(db *gorm.DB) {
			if v, ok := db.InstanceGet(dbTraceFinishKey); ok {
				if finish, ok := v.(func(error)); ok {
					finish(db.Error)
				}
			}
		})
		if err != nil {
			return fmt.Errorf("failed to register dbtrace:after_raw: %w", err)
		}
	}

	return nil
}
