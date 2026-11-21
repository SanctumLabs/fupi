//go:build legacy

package models

import "time"

type Url struct {
	BaseModel   BaseModel `bson:"inline"`
	UserId      string    `bson:"user_id" gorm:"user_id"`
	OriginalURL string    `bson:"original_url" gorm:"column:original_url"`
	ShortCode   string    `bson:"short_code" gorm:"column:short_code"`
	CustomAlias string    `bson:"custom_alias" gorm:"column:custom_alias"`
	ExpiresOn   time.Time `bson:"expires_on" gorm:"column:expires_on"`
	VisitCount  int       `bson:"visit_count" gorm:"column:visit_count"`
	Keywords    []Keyword `bson:"keywords" gorm:"-"`
}
