//go:build legacy

package models

// Keyword is model for keywords attached to a url
type Keyword struct {
	UrlId string `bson:"url_id" gorm:"column:url_id"`
	Value string `bson:"value" gorm:"column:value"`
}
