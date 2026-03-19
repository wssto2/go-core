package identity

import "errors"

var (
	ErrInvalidOIB       = errors.New("invalid OIB: checksum failed")
	ErrInvalidOIBLength = errors.New("invalid OIB: must be exactly 11 digits")
	ErrInvalidCompanyID = errors.New("invalid company ID")
)
