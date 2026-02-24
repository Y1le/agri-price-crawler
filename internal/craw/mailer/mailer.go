package mailer

import v1 "github.com/Y1le/agri-price-crawler/pkg/api/v1"

type Email struct {
	To      string
	Subject string
	Text    string
}

// type Recipient struct {
// 	ID uint64

// 	Email string
// 	Name  string

// 	FavoriteFoods string
// 	DislikeFoods  string
// }

type Mailer interface {
	Send(email Email) error
	SendBulkEmails(recipients []*v1.Subscribe, subject, htmlBody string) error
}
