//go:build legacy

package notifications

import (
	"errors"
	"fmt"
	"os"

	"github.com/sanctumlabs/curtz/app/internal/core/contracts"
)

const (
	NotificationTypeEmail = "email"
	NotificationTypeSMS   = "sms"
	NotificationTypePush  = "push"
)

type NotificationService struct {
	// baseUrl is used to include the host and port in messages for clickable links
	baseUrl  string
	emailSvc contracts.EmailService
	// smsSvc   SmsService
}

func NewNotificationSvc(host string, emailSvc contracts.EmailService) *NotificationService {
	return &NotificationService{
		emailSvc: emailSvc,
		baseUrl:  host,
	}
}

// SendNotification sends a notification message to given recipient with provided message
func (n *NotificationService) SendNotification(recipient, message, notifyType string) error {
	switch notifyType {
	case NotificationTypeEmail:
		return n.emailSvc.SendEmail(recipient, "", message)
	case NotificationTypeSMS:
		// return n.smsSvc.SendSms(recipient, message)
		panic("implement me")
	case NotificationTypePush:
		// return n.pushSvc.SendPush(recipient, message)
		panic("implement me")
	default:
		return errors.New("unknown notification type")
	}
}

func (n *NotificationService) SendEmailNotification(recipient, subject, message string) error {
	return n.emailSvc.SendEmail(recipient, subject, message)
}

func (n *NotificationService) SendEmailVerificationNotification(recipient, token string) error {
	baseUrl, err := os.Hostname()
	if err != nil {
		baseUrl = n.baseUrl
	}
	subject := "Welcome to Curtz, Kindly verify your account"
	message := fmt.Sprintf("Click on link %s/auth/verify/?v=%s to verify account", baseUrl, token)
	return n.emailSvc.SendEmail(recipient, subject, message)
}
