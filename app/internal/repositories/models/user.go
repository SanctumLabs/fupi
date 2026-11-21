//go:build legacy

package models

import (
	"time"
)

type User struct {
	BaseModel            BaseModel `bson:"inline"`
	Email                string    `bson:"email" gorm:"column:email,uniqueIndex"`
	Password             string    `bson:"password" gorm:"column:password"`
	ResetPasswordExpires time.Time `bson:"reset_password_expires" gorm:"column:reset_password_expires"`
	ResetPasswordToken   string    `bson:"reset_password_token" gorm:"column:reset_password_token"`
	VerificationExpires  time.Time `bson:"verification_expires" gorm:"column:verification_expires"`
	VerificationToken    string    `bson:"verification_token" gorm:"column:verification_token"`
	Verified             bool      `bson:"verified" gorm:"column:verified,omitempty"`
}
