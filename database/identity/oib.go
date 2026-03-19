package identity

import (
	"context"
	"database/sql/driver"
	"fmt"
	"strconv"
	"unicode"

	"github.com/goccy/go-json"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

type OIB struct {
	value *string
}

// --- Construction ---

func NewOIB(value string) (OIB, error) {
	if err := validateOIB(value); err != nil {
		return OIB{}, err
	}
	return OIB{value: &value}, nil
}

// MustOIB panics on invalid input — use only in tests or seeding
func MustOIB(value string) OIB {
	o, err := NewOIB(value)
	if err != nil {
		panic(fmt.Sprintf("MustOIB: %v", err))
	}
	return o
}

func NewNullOIB() OIB {
	return OIB{value: nil}
}

// --- Validation ---

func validateOIB(value string) error {
	if len(value) != 11 {
		return ErrInvalidOIBLength
	}
	for _, c := range value {
		if !unicode.IsDigit(c) {
			return ErrInvalidOIBLength
		}
	}
	if !oibChecksumValid(value) {
		return ErrInvalidOIB
	}
	return nil
}

// ISO 7064, MOD 11,10
func oibChecksumValid(oib string) bool {
	remainder := 10
	for i := 0; i < 10; i++ {
		digit, _ := strconv.Atoi(string(oib[i]))
		remainder = (remainder + digit) % 10
		if remainder == 0 {
			remainder = 10
		}
		remainder = (remainder * 2) % 11
	}
	control := 11 - remainder
	if control == 10 {
		return false
	}
	if control == 11 {
		control = 0
	}
	lastDigit, _ := strconv.Atoi(string(oib[10]))
	return control == lastDigit
}

// --- Accessors ---

func (o *OIB) Get() string {
	if o.value == nil {
		panic("OIB.Get() called on null value — check IsNull() first")
	}
	return *o.value
}

func (o *OIB) GetOrEmpty() string {
	if o.value == nil {
		return ""
	}
	return *o.value
}

func (o *OIB) IsNull() bool { return o.value == nil }
func (o *OIB) IsSet() bool  { return o.value != nil }

// Set validates before assignment — invalid OIB is rejected
func (o *OIB) Set(value string) error {
	if value == "" {
		o.value = nil
		return nil
	}
	if err := validateOIB(value); err != nil {
		return err
	}
	o.value = &value
	return nil
}

func (o *OIB) SetNull() {
	o.value = nil
}

// --- GORM ---

func (o OIB) GormDataType() string { return "varchar(11)" }

func (o OIB) GormDBDataType(db *gorm.DB, field *schema.Field) string {
	switch db.Name() {
	case "sqlite":
		return "TEXT"
	default:
		return "VARCHAR(11)"
	}
}

func (o OIB) GormValue(ctx context.Context, db *gorm.DB) clause.Expr {
	if o.value == nil {
		return clause.Expr{SQL: "NULL"}
	}
	return clause.Expr{SQL: "?", Vars: []interface{}{*o.value}}
}

func (o OIB) Value() (driver.Value, error) {
	if o.value == nil {
		return nil, nil
	}
	return *o.value, nil
}

func (o *OIB) Scan(value interface{}) error {
	if value == nil {
		o.value = nil
		return nil
	}
	var str string
	switch v := value.(type) {
	case string:
		str = v
	case []byte:
		str = string(v)
	default:
		return fmt.Errorf("OIB.Scan: unsupported type %T", value)
	}
	// Scan from DB — trust the stored value, skip checksum
	// (legacy data may exist that fails checksum)
	o.value = &str
	return nil
}

// --- JSON ---

func (o OIB) MarshalJSON() ([]byte, error) {
	if o.value == nil {
		return []byte("null"), nil
	}
	return json.Marshal(*o.value)
}

func (o *OIB) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		o.value = nil
		return nil
	}
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return fmt.Errorf("OIB.UnmarshalJSON: %w", err)
	}
	// JSON input from API — validate strictly
	return o.Set(str)
}

// --- Display ---

func (o *OIB) String() string { return o.GetOrEmpty() }
