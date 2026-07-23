package smtp

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"mime"
	"net/mail"
	netsmtp "net/smtp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Y1le/agri-price-crawler/internal/platform/config"
)

const (
	testRecipient = "person@example.com"
	testCode      = "123456"
)

func TestSenderSendsOneSafePlainTextMessageInProtocolOrder(t *testing.T) {
	t.Parallel()

	client := newFakeClient()
	sender, err := newSender(testConfig(), func(context.Context, config.SMTP) (smtpClient, error) {
		return client, nil
	})
	if err != nil {
		t.Fatalf("new sender: %v", err)
	}

	if err := sender.SendCode(context.Background(), testRecipient, testCode, 10*time.Minute); err != nil {
		t.Fatalf("send code: %v", err)
	}

	wantEvents := []string{
		"hello:localhost",
		"starttls:smtp.example.com",
		"auth",
		"mail:sender@example.com",
		"rcpt:" + testRecipient,
		"data",
		"data-close",
		"quit",
	}
	if got := client.Events(); !equalStrings(got, wantEvents) {
		t.Fatalf("protocol events = %#v, want %#v", got, wantEvents)
	}
	if client.rcptCalls != 1 {
		t.Fatalf("recipient calls = %d, want 1", client.rcptCalls)
	}

	message, err := mail.ReadMessage(bytes.NewReader(client.Message()))
	if err != nil {
		t.Fatalf("parse message: %v", err)
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(message.Header.Get("Subject"))
	if err != nil {
		t.Fatalf("decode subject: %v", err)
	}
	if subject != messageSubject {
		t.Fatalf("subject = %q, want %q", subject, messageSubject)
	}
	if got := message.Header.Get("From"); got != "sender@example.com" {
		t.Fatalf("From = %q", got)
	}
	if got := message.Header.Get("To"); got != testRecipient {
		t.Fatalf("To = %q", got)
	}
	body, err := io.ReadAll(message.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	const wantBody = "您的验证码是 123456，有效期 10 分钟。请勿向任何人泄露此验证码。"
	if got := strings.TrimSuffix(string(body), "\r\n"); got != wantBody {
		t.Fatalf("body = %q, want exact %q", got, wantBody)
	}
}

func TestSenderImplicitTLSDoesNotRequestSTARTTLS(t *testing.T) {
	t.Parallel()

	client := newFakeClient()
	cfg := testConfig()
	cfg.Port = 465
	cfg.TLSMode = "implicit"
	sender, err := newSender(cfg, func(context.Context, config.SMTP) (smtpClient, error) {
		return client, nil
	})
	if err != nil {
		t.Fatalf("new sender: %v", err)
	}
	if err := sender.SendCode(context.Background(), testRecipient, testCode, time.Minute); err != nil {
		t.Fatalf("send code: %v", err)
	}
	for _, event := range client.Events() {
		if strings.HasPrefix(event, "starttls:") {
			t.Fatalf("implicit TLS unexpectedly requested STARTTLS: %#v", client.Events())
		}
	}
}

func TestSenderRejectsCRLFBeforeDial(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		from      string
		recipient string
		subject   string
	}{
		{name: "from carriage return", from: "sender@example.com\rBcc: victim@example.com", recipient: testRecipient, subject: messageSubject},
		{name: "recipient newline", from: "sender@example.com", recipient: "person@example.com\nBcc: victim@example.com", subject: messageSubject},
		{name: "subject newline", from: "sender@example.com", recipient: testRecipient, subject: "safe\nBcc: victim@example.com"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dials := 0
			cfg := testConfig()
			cfg.From = tt.from
			sender, err := newSender(cfg, func(context.Context, config.SMTP) (smtpClient, error) {
				dials++
				return newFakeClient(), nil
			})
			if strings.Contains(tt.from, "\r") || strings.Contains(tt.from, "\n") {
				if err == nil {
					t.Fatal("newSender accepted unsafe From")
				}
				if dials != 0 {
					t.Fatalf("dial count = %d, want 0", dials)
				}
				return
			}
			if err != nil {
				t.Fatalf("new sender: %v", err)
			}

			if tt.subject != messageSubject {
				_, err = buildMessage(tt.from, tt.recipient, tt.subject, testCode, time.Minute)
			} else {
				err = sender.SendCode(context.Background(), tt.recipient, testCode, time.Minute)
			}
			if err == nil {
				t.Fatal("unsafe header value was accepted")
			}
			if dials != 0 {
				t.Fatalf("dial count = %d, want 0", dials)
			}
		})
	}
}

func TestSenderBoundsExchangeByTimeoutAndCancellation(t *testing.T) {
	t.Parallel()

	t.Run("timeout", func(t *testing.T) {
		cfg := testConfig()
		cfg.Timeout = 30 * time.Millisecond
		client := newFakeClient()
		client.blockAuth = true
		sender, err := newSender(cfg, func(ctx context.Context, got config.SMTP) (smtpClient, error) {
			deadline, ok := ctx.Deadline()
			if !ok {
				t.Fatal("factory context has no deadline")
			}
			remaining := time.Until(deadline)
			if remaining <= 0 || remaining > got.Timeout {
				t.Fatalf("factory deadline remaining = %s, timeout = %s", remaining, got.Timeout)
			}
			return client, nil
		})
		if err != nil {
			t.Fatalf("new sender: %v", err)
		}

		start := time.Now()
		err = sender.SendCode(context.Background(), testRecipient, testCode, time.Minute)
		if err == nil {
			t.Fatal("SendCode returned nil after timeout")
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("SendCode error = %v, want context deadline exceeded", err)
		}
		if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
			t.Fatalf("timeout took %s", elapsed)
		}
		if !client.Closed() {
			t.Fatal("timeout did not close SMTP client")
		}
	})

	t.Run("already cancelled", func(t *testing.T) {
		dials := 0
		sender, err := newSender(testConfig(), func(context.Context, config.SMTP) (smtpClient, error) {
			dials++
			return newFakeClient(), nil
		})
		if err != nil {
			t.Fatalf("new sender: %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err = sender.SendCode(ctx, testRecipient, testCode, time.Minute)
		if err == nil {
			t.Fatal("SendCode returned nil for cancelled context")
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("SendCode error = %v, want context canceled", err)
		}
		if dials != 0 {
			t.Fatalf("dial count = %d, want 0", dials)
		}
	})
}

func TestSenderErrorsDoNotDiscloseRecipientCodeOrProviderError(t *testing.T) {
	t.Parallel()

	client := newFakeClient()
	client.rcptErr = errors.New("provider rejected " + testRecipient + " with " + testCode)
	sender, err := newSender(testConfig(), func(context.Context, config.SMTP) (smtpClient, error) {
		return client, nil
	})
	if err != nil {
		t.Fatalf("new sender: %v", err)
	}
	err = sender.SendCode(context.Background(), testRecipient, testCode, time.Minute)
	if err == nil {
		t.Fatal("SendCode returned nil")
	}
	for _, secret := range []string{testRecipient, testCode, "provider rejected"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error %q discloses %q", err, secret)
		}
	}
}

func TestMemorySenderCodesAreDefensiveAndConcurrentSafe(t *testing.T) {
	t.Parallel()

	sender := NewMemory()
	const sends = 24
	var wait sync.WaitGroup
	for range sends {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if err := sender.SendCode(context.Background(), testRecipient, testCode, time.Minute); err != nil {
				t.Errorf("send code: %v", err)
			}
		}()
	}
	wait.Wait()

	codes := sender.Codes(testRecipient)
	if len(codes) != sends {
		t.Fatalf("codes count = %d, want %d", len(codes), sends)
	}
	codes[0] = "changed"
	if got := sender.Codes(testRecipient)[0]; got != testCode {
		t.Fatalf("stored code changed through returned slice: %q", got)
	}
	if got := sender.Codes("missing@example.com"); got == nil || len(got) != 0 {
		t.Fatalf("missing codes = %#v, want non-nil empty slice", got)
	}
}

func TestNewRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*config.SMTP)
	}{
		{name: "host", mutate: func(cfg *config.SMTP) { cfg.Host = "" }},
		{name: "port", mutate: func(cfg *config.SMTP) { cfg.Port = 0 }},
		{name: "username", mutate: func(cfg *config.SMTP) { cfg.Username = "" }},
		{name: "password", mutate: func(cfg *config.SMTP) { cfg.Password = "" }},
		{name: "from", mutate: func(cfg *config.SMTP) { cfg.From = "" }},
		{name: "TLS mode", mutate: func(cfg *config.SMTP) { cfg.TLSMode = "invalid" }},
		{name: "timeout", mutate: func(cfg *config.SMTP) { cfg.Timeout = 0 }},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := testConfig()
			tt.mutate(&cfg)
			if _, err := New(cfg); err == nil {
				t.Fatal("New accepted invalid configuration")
			}
		})
	}
}

func testConfig() config.SMTP {
	return config.SMTP{
		Host:     "smtp.example.com",
		Port:     587,
		Username: "smtp-user",
		Password: "smtp-password",
		From:     "sender@example.com",
		TLSMode:  "starttls",
		Timeout:  time.Second,
	}
}

type fakeClient struct {
	mu        sync.Mutex
	events    []string
	message   bytes.Buffer
	rcptCalls int
	rcptErr   error
	blockAuth bool
	closed    chan struct{}
	closeOnce sync.Once
}

func newFakeClient() *fakeClient {
	return &fakeClient{closed: make(chan struct{})}
}

func (client *fakeClient) Hello(name string) error {
	client.addEvent("hello:" + name)
	return nil
}

func (client *fakeClient) StartTLS(cfg *tls.Config) error {
	client.addEvent("starttls:" + cfg.ServerName)
	return nil
}

func (client *fakeClient) Auth(netsmtp.Auth) error {
	client.addEvent("auth")
	if client.blockAuth {
		<-client.closed
		return errors.New("closed")
	}
	return nil
}

func (client *fakeClient) Mail(from string) error {
	client.addEvent("mail:" + from)
	return nil
}

func (client *fakeClient) Rcpt(to string) error {
	client.mu.Lock()
	client.rcptCalls++
	client.mu.Unlock()
	client.addEvent("rcpt:" + to)
	return client.rcptErr
}

func (client *fakeClient) Data() (io.WriteCloser, error) {
	client.addEvent("data")
	return &fakeDataWriter{client: client}, nil
}

func (client *fakeClient) Quit() error {
	client.addEvent("quit")
	return nil
}

func (client *fakeClient) Close() error {
	client.closeOnce.Do(func() { close(client.closed) })
	return nil
}

func (client *fakeClient) addEvent(event string) {
	client.mu.Lock()
	defer client.mu.Unlock()
	client.events = append(client.events, event)
}

func (client *fakeClient) Events() []string {
	client.mu.Lock()
	defer client.mu.Unlock()
	return append([]string(nil), client.events...)
}

func (client *fakeClient) Message() []byte {
	client.mu.Lock()
	defer client.mu.Unlock()
	return append([]byte(nil), client.message.Bytes()...)
}

func (client *fakeClient) Closed() bool {
	select {
	case <-client.closed:
		return true
	default:
		return false
	}
}

type fakeDataWriter struct {
	client *fakeClient
}

func (writer *fakeDataWriter) Write(p []byte) (int, error) {
	writer.client.mu.Lock()
	defer writer.client.mu.Unlock()
	return writer.client.message.Write(p)
}

func (writer *fakeDataWriter) Close() error {
	writer.client.addEvent("data-close")
	return nil
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
