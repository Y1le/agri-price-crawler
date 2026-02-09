package mailer

type Email struct {
	To      string
	Subject string
	Text    string
}

type Mailer interface {
	Send(email Email) error
}
