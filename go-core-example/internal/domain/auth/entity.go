package auth

import "github.com/wssto2/go-core/auth"

// AppUserData is the app-specific payload attached to every authenticated user.
// Stored in auth.User[AppUserData].Data — not in the JWT, loaded from DB.
type AppUserData struct {
	Role     string `json:"role"      gorm:"-"`
	DealerID int    `json:"dealer_id" gorm:"-"`
}

// AppUser is the concrete GORM-backed user type for this application.
// It embeds auth.User[AppUserData] and adds DB-persisted fields.
type AppUser struct {
	auth.User[AppUserData]

	// DB columns beyond the base User fields.
	PasswordHash string   `json:"-"          gorm:"size:255;not null"`
	Policies     []string `json:"policies"   gorm:"type:json;serializer:json"`
}
