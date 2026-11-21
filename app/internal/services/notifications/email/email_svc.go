//go:build legacy

package email

type EmailService struct{}

func NewEmailSvc() *EmailService {
	return &EmailService{}
}

func (e *EmailService) SendEmail(recipient, subject, body string) error {
	return nil
	// panic("implement me")
}
