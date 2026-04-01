package datatable

import "gorm.io/gorm"

type View struct {
	URIKey string
	Query  func(query *gorm.DB, tableName string) *gorm.DB
}

func NewView(key string, fn func(q *gorm.DB, t string) *gorm.DB) View {
	return View{URIKey: key, Query: fn}
}
