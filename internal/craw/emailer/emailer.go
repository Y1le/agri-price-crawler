package mailer

type Email struct {
	To      string
	Subject string
	Text    string
}

type Recipient struct {
	Email string
	Name  string
}

type Mailer interface {
	Send(email Email) error
	SendBulkEmails(recipients []Recipient, subject, htmlBody string) error
}
