package mailer

import (
	"fmt"
	"net/smtp"

	"github.com/jordan-wright/email"
)

type SMTPMailer struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
}

var (
	Instance *SMTPMailer
)

func GetInstance() *SMTPMailer {
	if Instance == nil {
		panic("SMTPMailer not initialized. Call Init() first.")
	}
	return Instance
}

func (s *SMTPMailer) Send(e Email) error {
	em := email.NewEmail()
	em.From = s.From
	em.To = []string{e.To}
	em.Subject = e.Subject
	em.Text = []byte(e.Text)

	addr := fmt.Sprintf("%s:%d", s.Host, s.Port)
	auth := smtp.PlainAuth("", s.Username, s.Password, s.Host)
	return em.Send(addr, auth)
}
