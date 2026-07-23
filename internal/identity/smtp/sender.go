// Package smtp delivers identity verification codes over SMTP.
package smtp

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	netsmtp "net/smtp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Y1le/agri-price-crawler/internal/identity"
	"github.com/Y1le/agri-price-crawler/internal/platform/config"
)

const (
	messageSubject = "农产品价格助手登录验证码"
	helloName      = "localhost"
)

type smtpClient interface {
	Hello(string) error
	StartTLS(*tls.Config) error
	Auth(netsmtp.Auth) error
	Mail(string) error
	Rcpt(string) error
	Data() (io.WriteCloser, error)
	Quit() error
	Close() error
}

type clientFactory func(context.Context, config.SMTP) (smtpClient, error)

// Sender sends verification codes using a timeout-bounded SMTP exchange.
type Sender struct {
	config    config.SMTP
	newClient clientFactory
}

var _ identity.EmailSender = (*Sender)(nil)

// New builds an SMTP-backed verification-code sender.
func New(smtpConfig config.SMTP) (*Sender, error) {
	return newSender(smtpConfig, dialSMTPClient)
}

func newSender(smtpConfig config.SMTP, factory clientFactory) (*Sender, error) {
	if smtpConfig.Host == "" {
		return nil, errors.New("create SMTP sender: host is required")
	}
	if smtpConfig.Port < 1 || smtpConfig.Port > 65535 {
		return nil, errors.New("create SMTP sender: port is invalid")
	}
	if smtpConfig.Username == "" {
		return nil, errors.New("create SMTP sender: username is required")
	}
	if smtpConfig.Password == "" {
		return nil, errors.New("create SMTP sender: password is required")
	}
	if smtpConfig.From == "" {
		return nil, errors.New("create SMTP sender: sender is required")
	}
	if !safeHeaderValue(smtpConfig.From) {
		return nil, errors.New("create SMTP sender: sender is invalid")
	}
	switch smtpConfig.TLSMode {
	case "implicit", "starttls", "none":
	default:
		return nil, errors.New("create SMTP sender: TLS mode is invalid")
	}
	if smtpConfig.Timeout <= 0 {
		return nil, errors.New("create SMTP sender: timeout must be positive")
	}
	if factory == nil {
		return nil, errors.New("create SMTP sender: client factory is required")
	}
	return &Sender{config: smtpConfig, newClient: factory}, nil
}

// SendCode sends a single plain-text verification-code message.
func (sender *Sender) SendCode(
	ctx context.Context,
	recipient string,
	code string,
	expiresIn time.Duration,
) error {
	if ctx == nil {
		return errors.New("send verification email: context is required")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("send verification email: %w", err)
	}

	message, err := buildMessage(sender.config.From, recipient, messageSubject, code, expiresIn)
	if err != nil {
		return errors.New("send verification email: invalid message")
	}

	exchangeCtx, cancel := context.WithTimeout(ctx, sender.config.Timeout)
	defer cancel()

	client, err := sender.newClient(exchangeCtx, sender.config)
	if err != nil {
		if contextErr := exchangeCtx.Err(); contextErr != nil {
			return fmt.Errorf("send verification email: %w", contextErr)
		}
		return errors.New("send verification email: SMTP connection failed")
	}
	if contextErr := exchangeCtx.Err(); contextErr != nil {
		_ = client.Close()
		return fmt.Errorf("send verification email: %w", contextErr)
	}

	stopWatching := closeOnContext(exchangeCtx, client)
	defer stopWatching()
	defer client.Close()

	if err := client.Hello(helloName); err != nil {
		return exchangeError(exchangeCtx, "SMTP greeting failed")
	}
	if sender.config.TLSMode == "starttls" {
		if err := client.StartTLS(tlsConfig(sender.config.Host)); err != nil {
			return exchangeError(exchangeCtx, "SMTP TLS negotiation failed")
		}
	}
	auth := netsmtp.PlainAuth(
		"",
		sender.config.Username,
		sender.config.Password,
		sender.config.Host,
	)
	if err := client.Auth(auth); err != nil {
		return exchangeError(exchangeCtx, "SMTP authentication failed")
	}
	if err := client.Mail(sender.config.From); err != nil {
		return exchangeError(exchangeCtx, "SMTP sender rejected")
	}
	if err := client.Rcpt(recipient); err != nil {
		return exchangeError(exchangeCtx, "SMTP recipient rejected")
	}
	writer, err := client.Data()
	if err != nil {
		return exchangeError(exchangeCtx, "SMTP data command failed")
	}
	if _, err := writer.Write(message); err != nil {
		_ = writer.Close()
		return exchangeError(exchangeCtx, "SMTP message write failed")
	}
	if err := writer.Close(); err != nil {
		return exchangeError(exchangeCtx, "SMTP message completion failed")
	}
	if err := client.Quit(); err != nil {
		return exchangeError(exchangeCtx, "SMTP quit failed")
	}
	if contextErr := exchangeCtx.Err(); contextErr != nil {
		return fmt.Errorf("send verification email: %w", contextErr)
	}
	return nil
}

func exchangeError(ctx context.Context, stage string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("send verification email: %w", err)
	}
	return fmt.Errorf("send verification email: %s", stage)
}

func buildMessage(
	from string,
	recipient string,
	subject string,
	code string,
	expiresIn time.Duration,
) ([]byte, error) {
	if !safeHeaderValue(from) || from == "" {
		return nil, errors.New("invalid sender header")
	}
	if !safeHeaderValue(recipient) || recipient == "" {
		return nil, errors.New("invalid recipient header")
	}
	if !safeHeaderValue(subject) || subject == "" {
		return nil, errors.New("invalid subject header")
	}
	if expiresIn <= 0 {
		return nil, errors.New("invalid expiry")
	}

	var message bytes.Buffer
	fmt.Fprintf(&message, "From: %s\r\n", from)
	fmt.Fprintf(&message, "To: %s\r\n", recipient)
	fmt.Fprintf(&message, "Subject: %s\r\n", mime.QEncoding.Encode("UTF-8", subject))
	message.WriteString("MIME-Version: 1.0\r\n")
	message.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	message.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	message.WriteString("\r\n")
	fmt.Fprintf(
		&message,
		"您的验证码是 %s，有效期 %d 分钟。请勿向任何人泄露此验证码。\r\n",
		code,
		expiresIn/time.Minute,
	)
	return message.Bytes(), nil
}

func safeHeaderValue(value string) bool {
	return !strings.ContainsAny(value, "\r\n")
}

func dialSMTPClient(ctx context.Context, smtpConfig config.SMTP) (smtpClient, error) {
	address := net.JoinHostPort(smtpConfig.Host, strconv.Itoa(smtpConfig.Port))
	connection, err := (&net.Dialer{Timeout: smtpConfig.Timeout}).DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, errors.New("dial SMTP server")
	}

	stopWatching := closeOnContext(ctx, connection)
	defer stopWatching()
	keepConnection := false
	defer func() {
		if !keepConnection {
			_ = connection.Close()
		}
	}()

	deadline := time.Now().Add(smtpConfig.Timeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := connection.SetDeadline(deadline); err != nil {
		return nil, errors.New("set SMTP connection deadline")
	}

	if smtpConfig.TLSMode == "implicit" {
		tlsConnection := tls.Client(connection, tlsConfig(smtpConfig.Host))
		if err := tlsConnection.HandshakeContext(ctx); err != nil {
			return nil, errors.New("negotiate implicit SMTP TLS")
		}
		connection = tlsConnection
	}

	client, err := netsmtp.NewClient(connection, smtpConfig.Host)
	if err != nil {
		return nil, errors.New("create SMTP client")
	}
	keepConnection = true
	return client, nil
}

func tlsConfig(host string) *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: host,
	}
}

func closeOnContext(ctx context.Context, closer io.Closer) func() {
	stop := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		select {
		case <-ctx.Done():
			_ = closer.Close()
		case <-stop:
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			close(stop)
			<-stopped
		})
	}
}

// MemorySender stores codes in memory for local development and tests.
type MemorySender struct {
	mu    sync.RWMutex
	codes map[string][]string
}

var _ identity.EmailSender = (*MemorySender)(nil)

// NewMemory returns a concurrency-safe in-memory verification-code sender.
func NewMemory() *MemorySender {
	return &MemorySender{codes: make(map[string][]string)}
}

// SendCode records a code without writing it to logs.
func (sender *MemorySender) SendCode(
	ctx context.Context,
	email string,
	code string,
	_ time.Duration,
) error {
	if ctx == nil {
		return errors.New("record verification email: context is required")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("record verification email: %w", err)
	}
	sender.mu.Lock()
	defer sender.mu.Unlock()
	sender.codes[email] = append(sender.codes[email], code)
	return nil
}

// Codes returns a defensive copy of the codes recorded for email.
func (sender *MemorySender) Codes(email string) []string {
	sender.mu.RLock()
	defer sender.mu.RUnlock()
	codes := sender.codes[email]
	result := make([]string, len(codes))
	copy(result, codes)
	return result
}
