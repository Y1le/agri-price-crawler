package smtp

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/Y1le/agri-price-crawler/internal/platform/config"
)

func TestDialSMTPClientLoopbackTLSModes(t *testing.T) {
	certificate, roots := newTestCertificate(t, "localhost")

	for _, mode := range []string{"implicit", "starttls"} {
		t.Run(mode, func(t *testing.T) {
			server := newLoopbackSMTPServer(t, mode, certificate)
			defer server.Close()

			cfg := testConfig()
			cfg.Host = "localhost"
			cfg.Port = server.Port()
			cfg.TLSMode = mode
			cfg.Timeout = time.Second
			clientTLS := func(string) *tls.Config {
				return &tls.Config{
					MinVersion: tls.VersionTLS12,
					RootCAs:    roots,
					ServerName: "localhost",
				}
			}
			sender, err := newSenderWithTLSConfig(
				cfg,
				func(ctx context.Context, got config.SMTP) (smtpClient, error) {
					return dialSMTPClientWithTLSConfig(ctx, got, clientTLS(got.Host))
				},
				clientTLS,
			)
			if err != nil {
				t.Fatalf("new sender: %v", err)
			}

			if err := sender.SendCode(context.Background(), testRecipient, testCode, time.Minute); err != nil {
				t.Fatalf("send over %s TLS: %v", mode, err)
			}
			select {
			case message := <-server.messages:
				if !strings.Contains(message, "Subject:") {
					t.Fatalf("server message has no Subject header: %q", message)
				}
			case err := <-server.errs:
				t.Fatalf("SMTP server: %v", err)
			case <-time.After(time.Second):
				t.Fatal("SMTP server did not receive message")
			}
		})
	}
}

func TestDialSMTPClientRejectsWrongHostAndUntrustedCertificate(t *testing.T) {
	certificate, roots := newTestCertificate(t, "localhost")

	tests := []struct {
		name      string
		clientTLS func() *tls.Config
	}{
		{
			name: "wrong server name",
			clientTLS: func() *tls.Config {
				return &tls.Config{
					MinVersion: tls.VersionTLS12,
					RootCAs:    roots,
					ServerName: "wrong.example",
				}
			},
		},
		{
			name: "untrusted CA",
			clientTLS: func() *tls.Config {
				return &tls.Config{
					MinVersion: tls.VersionTLS12,
					RootCAs:    x509.NewCertPool(),
					ServerName: "localhost",
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newLoopbackSMTPServer(t, "implicit", certificate)
			defer server.Close()

			cfg := testConfig()
			cfg.Host = "localhost"
			cfg.Port = server.Port()
			cfg.TLSMode = "implicit"
			cfg.Timeout = time.Second
			sender, err := newSenderWithTLSConfig(
				cfg,
				func(ctx context.Context, got config.SMTP) (smtpClient, error) {
					return dialSMTPClientWithTLSConfig(ctx, got, tt.clientTLS())
				},
				func(string) *tls.Config { return tt.clientTLS() },
			)
			if err != nil {
				t.Fatalf("new sender: %v", err)
			}
			if err := sender.SendCode(context.Background(), testRecipient, testCode, time.Minute); err == nil {
				t.Fatal("SendCode accepted invalid server certificate")
			}
		})
	}
}

func TestDialSMTPClientDeadlineClosesStalledConnection(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	connectionClosed := make(chan error, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			connectionClosed <- acceptErr
			return
		}
		defer connection.Close()
		_, readErr := connection.Read(make([]byte, 1))
		connectionClosed <- readErr
	}()

	cfg := testConfig()
	cfg.Host = "127.0.0.1"
	cfg.Port = listener.Addr().(*net.TCPAddr).Port
	cfg.TLSMode = "none"
	cfg.Timeout = 30 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()

	start := time.Now()
	_, err = dialSMTPClient(ctx, cfg)
	if err == nil {
		t.Fatal("dialSMTPClient succeeded without SMTP greeting")
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("deadline took %s", elapsed)
	}
	select {
	case readErr := <-connectionClosed:
		if readErr == nil {
			t.Fatal("stalled server read succeeded, want closed connection")
		}
	case <-time.After(time.Second):
		t.Fatal("client did not close stalled connection")
	}
}

type loopbackSMTPServer struct {
	listener net.Listener
	mode     string
	config   *tls.Config
	messages chan string
	errs     chan error
}

func newLoopbackSMTPServer(t *testing.T, mode string, certificate tls.Certificate) *loopbackSMTPServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := &loopbackSMTPServer{
		listener: listener,
		mode:     mode,
		config: &tls.Config{
			Certificates: []tls.Certificate{certificate},
			MinVersion:   tls.VersionTLS12,
		},
		messages: make(chan string, 1),
		errs:     make(chan error, 1),
	}
	go server.serveOne()
	return server
}

func (server *loopbackSMTPServer) Port() int {
	return server.listener.Addr().(*net.TCPAddr).Port
}

func (server *loopbackSMTPServer) Close() {
	_ = server.listener.Close()
}

func (server *loopbackSMTPServer) serveOne() {
	connection, err := server.listener.Accept()
	if err != nil {
		server.report(err)
		return
	}
	defer connection.Close()

	tlsActive := false
	if server.mode == "implicit" {
		tlsConnection := tls.Server(connection, server.config)
		if err := tlsConnection.Handshake(); err != nil {
			server.report(err)
			return
		}
		connection = tlsConnection
		tlsActive = true
	}
	reader := bufio.NewReader(connection)
	writer := bufio.NewWriter(connection)
	if err := writeSMTPReply(writer, "220 localhost ESMTP ready"); err != nil {
		server.report(err)
		return
	}

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if !errors.Is(err, io.EOF) {
				server.report(err)
			}
			return
		}
		command := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		verb := strings.ToUpper(strings.SplitN(command, " ", 2)[0])
		switch verb {
		case "EHLO", "HELO":
			reply := "250-localhost\r\n"
			if server.mode == "starttls" && !tlsActive {
				reply += "250-STARTTLS\r\n"
			}
			reply += "250-AUTH PLAIN\r\n250 SMTPUTF8\r\n"
			if _, err := writer.WriteString(reply); err != nil {
				server.report(err)
				return
			}
			if err := writer.Flush(); err != nil {
				server.report(err)
				return
			}
		case "STARTTLS":
			if server.mode != "starttls" || tlsActive {
				server.report(fmt.Errorf("unexpected STARTTLS"))
				return
			}
			if err := writeSMTPReply(writer, "220 ready for TLS"); err != nil {
				server.report(err)
				return
			}
			tlsConnection := tls.Server(connection, server.config)
			if err := tlsConnection.Handshake(); err != nil {
				server.report(err)
				return
			}
			connection = tlsConnection
			reader = bufio.NewReader(connection)
			writer = bufio.NewWriter(connection)
			tlsActive = true
		case "AUTH":
			if !tlsActive {
				server.report(fmt.Errorf("credentials sent before TLS"))
				return
			}
			if err := writeSMTPReply(writer, "235 authentication successful"); err != nil {
				server.report(err)
				return
			}
		case "MAIL", "RCPT":
			if err := writeSMTPReply(writer, "250 accepted"); err != nil {
				server.report(err)
				return
			}
		case "DATA":
			if err := writeSMTPReply(writer, "354 end with dot"); err != nil {
				server.report(err)
				return
			}
			var message strings.Builder
			for {
				dataLine, err := reader.ReadString('\n')
				if err != nil {
					server.report(err)
					return
				}
				if dataLine == ".\r\n" {
					break
				}
				message.WriteString(dataLine)
			}
			if err := writeSMTPReply(writer, "250 queued"); err != nil {
				server.report(err)
				return
			}
			server.messages <- message.String()
		case "QUIT":
			_ = writeSMTPReply(writer, "221 bye")
			return
		default:
			server.report(fmt.Errorf("unexpected SMTP command %q", command))
			return
		}
	}
}

func (server *loopbackSMTPServer) report(err error) {
	select {
	case server.errs <- err:
	default:
	}
}

func writeSMTPReply(writer *bufio.Writer, reply string) error {
	if _, err := writer.WriteString(reply + "\r\n"); err != nil {
		return err
	}
	return writer.Flush()
}

func newTestCertificate(t *testing.T, serverName string) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	_, caKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate CA key: %v", err)
	}
	now := time.Now()
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Identity SMTP Test CA"},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, caKey.Public(), caKey)
	if err != nil {
		t.Fatalf("create CA: %v", err)
	}
	ca, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatalf("parse CA: %v", err)
	}

	_, serverKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate server key: %v", err)
	}
	serverTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: serverName},
		DNSNames:     []string{serverName},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.Add(time.Hour),
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, ca, serverKey.Public(), caKey)
	if err != nil {
		t.Fatalf("create server certificate: %v", err)
	}
	certificate, err := tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverDER}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: mustPKCS8(t, serverKey)}),
	)
	if err != nil {
		t.Fatalf("load server certificate: %v", err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	return certificate, roots
}

func mustPKCS8(t *testing.T, key ed25519.PrivateKey) []byte {
	t.Helper()
	encoded, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal server key: %v", err)
	}
	return encoded
}
