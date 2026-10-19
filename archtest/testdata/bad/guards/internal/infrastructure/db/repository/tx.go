package repository

import "gorm.io/gorm"

type repo struct{ db *gorm.DB }

func (r repo) nested() error {
	return r.db.Transaction(func(tx *gorm.DB) error { return nil })
}

func (r repo) one() error {
	return r.db.WithinTx(nil, func() error { return nil })
}
