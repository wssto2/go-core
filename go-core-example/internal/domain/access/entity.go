package access

import "gorm.io/gorm"

// Dealer is a tenant.
type Dealer struct {
	ID   int    `gorm:"primaryKey" json:"id"`
	Name string `gorm:"size:100;not null" json:"name"`
}

// Location belongs to a dealer.
type Location struct {
	ID       int    `gorm:"primaryKey" json:"id"`
	DealerID int    `gorm:"not null;index" json:"dealer_id"`
	Name     string `gorm:"size:100;not null" json:"name"`
}

// Staff records a user's own location, which the OwnLocation qualifier uses.
type Staff struct {
	UserID     int `gorm:"primaryKey"`
	LocationID int `gorm:"not null"`
}

// Lead is a sales lead. It has a dealer and a location, an optional owner (the
// salesperson it is assigned to; NULL is the unassigned pool) and a vehicle kind.
type Lead struct {
	ID         int    `gorm:"primaryKey" json:"id"`
	DealerID   int    `gorm:"not null;index" json:"dealer_id"`
	LocationID int    `gorm:"not null;index" json:"location_id"`
	OwnerID    *int   `gorm:"index" json:"owner_id"`
	Kind       string `gorm:"size:10;not null" json:"kind"` // "used" or "new"
	Title      string `gorm:"size:200;not null" json:"title"`
}

// Migrate creates the example's tables.
func Migrate(db *gorm.DB) error {
	return db.AutoMigrate(&Dealer{}, &Location{}, &Staff{}, &Lead{})
}
