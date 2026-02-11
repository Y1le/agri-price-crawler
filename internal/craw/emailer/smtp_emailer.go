package mailer

import (
	"fmt"
	"log"
	"sync"

	"gopkg.in/gomail.v2"
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
var _ Mailer = (*SMTPMailer)(nil)

func GetInstance() *SMTPMailer {
	if Instance == nil {
		panic("SMTPMailer not initialized. Call Init() first.")
	}
	return Instance
}

func (s *SMTPMailer) Send(e Email) error {
	return nil
	// em := email.NewEmail()
	// em.From = s.From
	// em.To = []string{e.To}
	// em.Subject = e.Subject
	// em.Text = []byte(e.Text)

	// addr := fmt.Sprintf("%s:%d", s.Host, s.Port)
	// auth := smtp.PlainAuth("", s.Username, s.Password, s.Host)
	// return em.Send(addr, auth)
}

func (s *SMTPMailer) SendBulkEmails(recipients []Recipient, subject, htmlBody string) error {
	// 创建 SMTP 拨号器（连接池）
	dialer := gomail.NewDialer(s.Host, s.Port, s.Username, s.Password)

	sender, err := dialer.Dial()
	if err != nil {
		return fmt.Errorf("failed to dial SMTP: %w", err)
	}
	defer sender.Close()

	var wg sync.WaitGroup
	errChan := make(chan error, 1)

	maxWorkers := 5
	semaphore := make(chan struct{}, maxWorkers)

	for _, r := range recipients {
		wg.Add(1)
		go func(recipient Recipient) {
			defer wg.Done()

			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			m := gomail.NewMessage()
			m.SetHeader("From", s.From)
			m.SetAddressHeader("To", recipient.Email, recipient.Name)
			m.SetHeader("Subject", subject)
			m.SetBody("text/html", htmlBody)

			if err := gomail.Send(sender, m); err != nil {
				select {
				case errChan <- fmt.Errorf("failed to send to %s: %w", recipient.Email, err):
				default:
				}
				return
			}
			log.Printf("Sent to %s", recipient.Email)
		}(r)
	}

	wg.Wait()
	close(errChan)

	if err := <-errChan; err != nil {
		return err
	}
	return nil
}
