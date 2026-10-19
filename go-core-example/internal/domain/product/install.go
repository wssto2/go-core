// Package product is the example's catalogue feature: GORM entity, repository, service, typed routes and an
// event consumer that makes the image variants. It is installed with one call, Install; nothing is resolved from
// a container.
package product

import (
	"context"
	"fmt"

	"github.com/wssto2/go-core/audit"
	"github.com/wssto2/go-core/database"
	dbtypes "github.com/wssto2/go-core/database/types"
	"github.com/wssto2/go-core/gocore"
	storagelocal "github.com/wssto2/go-core/storage/local"
	"gorm.io/gorm"
)

// Install builds the feature over the App's collaborators, collects its routes and its image consumer, and returns
// the service for a feature that takes it as a plain argument (the page shell reads the catalogue from it).
func Install(app *gocore.App, storageDir string) Service {
	db := app.Database()
	tx := database.NewTransactor(db)

	// Local filesystem storage for product images. Swap for S3/GCS driver here.
	store, err := storagelocal.New(storageDir)
	if err != nil {
		app.Fail("product: storage: "+err.Error(), "set STORAGE_BASE_DIR to a directory the process can write")

		return nil
	}

	repo := NewRepository(db)
	svc := NewInstrumentedService(NewService(repo, tx, audit.NewRepository(tx), store, app.Logger()), app.Telemetry().Service)

	// Demo only: a real app creates these tables with migrations (app.Schema or app.Migrations) and seeds nothing.
	app.OnStart(func(ctx context.Context) error {
		if err := migrate(db.WithContext(ctx)); err != nil {
			return fmt.Errorf("product: migrate: %w", err)
		}

		return seedDemoCatalog(db.WithContext(ctx))
	})

	app.Events(newImageWorker(repo, store, app.Logger()).consumer())
	app.Routes(bind(svc)...)

	return svc
}

func migrate(db *gorm.DB) error {
	if err := db.AutoMigrate(&Product{}); err != nil {
		return err
	}

	return audit.Migrate(db)
}

func seedDemoCatalog(db *gorm.DB) error {
	var count int64
	if err := db.Model(&Product{}).Count(&count).Error; err != nil {
		return err
	}

	if count > 0 {
		return nil
	}

	products := []Product{
		{
			Name:        "Aurora Desk Lamp",
			SKU:         "AUR-LAMP-001",
			Description: dbtypes.NewNullString("Minimal aluminum desk lamp with warm ambient light."),
			Price:       dbtypes.NewFloat(79.00),
			Stock:       18,
			Active:      dbtypes.NewBool(true),
			CreatedBy:   1,
		},
		{
			Name:        "Nimbus Travel Mug",
			SKU:         "NMB-MUG-002",
			Description: dbtypes.NewNullString("Insulated stainless steel mug designed for daily commutes."),
			Price:       dbtypes.NewFloat(24.50),
			Stock:       42,
			Active:      dbtypes.NewBool(true),
			CreatedBy:   1,
		},
		{
			Name:        "Atlas Notebook",
			SKU:         "ATL-NOTE-003",
			Description: dbtypes.NewNullString("Hardcover dotted notebook for product planning and sketches."),
			Price:       dbtypes.NewFloat(18.90),
			Stock:       9,
			Active:      dbtypes.NewBool(true),
			CreatedBy:   1,
		},
		{
			Name:        "Summit Carry Tote",
			SKU:         "SUM-TOTE-004",
			Description: dbtypes.NewNullString("Heavy canvas tote for laptops, cables, and day-trip essentials."),
			Price:       dbtypes.NewFloat(36.00),
			Stock:       0,
			Active:      dbtypes.NewBool(true),
			CreatedBy:   1,
		},
	}

	return db.Create(&products).Error
}
