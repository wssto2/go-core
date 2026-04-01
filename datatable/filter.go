package datatable

import "gorm.io/gorm"

type Filter struct {
	URIKey string
	Query  func(query *gorm.DB, value string, tableName string) *gorm.DB
}

func NewFilter(key string, fn func(q *gorm.DB, v, t string) *gorm.DB) Filter {
	return Filter{URIKey: key, Query: fn}
}
