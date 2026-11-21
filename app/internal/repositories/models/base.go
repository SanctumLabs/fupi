//go:build legacy

package models

import (
	"time"
)

// BaseModel including other common fields
type BaseModel struct {
	Id        string    `bson:"id" gorm:"primary_key;column;column:id"`
	CreatedAt time.Time `bson:"created_at" gorm:"column:created_at"`
	UpdatedAt time.Time `bson:"updated_at" gorm:"column:updated_at"`
}
