package email

import (
	"bufio"
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSMTP is a minimal scripted SMTP server on 127.0.0.1 for one
// connection. It greets with 220, answers EHLO with caps, and accepts
// MAIL/RCPT/DATA/QUIT. With silent set it greets and then never answers.
type fakeSMTP struct {
	ln     net.Listener
	caps   []string
	silent bool

	mu   sync.Mutex
	cmds []string
	data string
}

func startFakeSMTP(t *testing.T, caps []string, silent bool) *fakeSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &fakeSMTP{ln: ln, caps: caps, silent: silent}
	t.Cleanup(func() { ln.Close() })
	go s.serve()
	return s
}

func (s *fakeSMTP) cfg() SMTPConfig {
	addr := s.ln.Addr().(*net.TCPAddr)
	return SMTPConfig{Host: "127.0.0.1", Port: addr.Port, From: "noreply@example.com"}
}

func (s *fakeSMTP) commands() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.cmds...)
}

func (s *fakeSMTP) received() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data
}

func (s *fakeSMTP) serve() {
	conn, err := s.ln.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	r := bufio.NewReader(conn)
	reply := func(line string) { conn.Write([]byte(line + "\r\n")) }

	reply("220 fake ESMTP")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		s.mu.Lock()
		s.cmds = append(s.cmds, line)
		s.mu.Unlock()
		if s.silent {
			continue // read and never answer
		}
		verb := strings.ToUpper(strings.SplitN(line, " ", 2)[0])
		switch verb {
		case "EHLO", "HELO":
			if len(s.caps) == 0 {
				reply("250 fake")
				continue
			}
			reply("250-fake")
			for i, c := range s.caps {
				if i == len(s.caps)-1 {
					reply("250 " + c)
				} else {
					reply("250-" + c)
				}
			}
		case "MAIL", "RCPT", "RSET", "NOOP":
			reply("250 OK")
		case "DATA":
			reply("354 go ahead")
			var b strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				b.WriteString(l)
			}
			s.mu.Lock()
			s.data = b.String()
			s.mu.Unlock()
			reply("250 queued")
		case "QUIT":
			reply("221 bye")
			return
		default:
			reply("502 not implemented")
		}
	}
}

func hasCommand(cmds []string, prefix string) bool {
	for _, c := range cmds {
		if strings.HasPrefix(strings.ToUpper(c), prefix) {
			return true
		}
	}
	return false
}

func TestSendViaSMTPRefusesWithoutSTARTTLS(t *testing.T) {
	srv := startFakeSMTP(t, []string{"8BITMIME"}, false)
	cfg := srv.cfg()

	err := sendViaSMTP(context.Background(), cfg, []string{"a@example.com"}, []byte("Subject: x\r\n\r\nbody\r\n"))
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("sendViaSMTP() error = %v, want one mentioning STARTTLS", err)
	}
	// Give the server a moment to record anything the client sent.
	time.Sleep(50 * time.Millisecond)
	if hasCommand(srv.commands(), "MAIL FROM") {
		t.Errorf("client sent MAIL FROM over cleartext: %v", srv.commands())
	}
}

func TestSendViaSMTPAllowPlaintextDelivers(t *testing.T) {
	srv := startFakeSMTP(t, []string{"8BITMIME"}, false)
	cfg := srv.cfg()
	cfg.AllowPlaintext = true

	err := sendViaSMTP(context.Background(), cfg, []string{"a@example.com"}, []byte("Subject: x\r\n\r\nbody\r\n"))
	if err != nil {
		t.Fatalf("sendViaSMTP() error = %v", err)
	}
	if !strings.Contains(srv.received(), "body") {
		t.Errorf("server did not receive the message, got %q", srv.received())
	}
}

func TestSendViaSMTPRefusesCredentialsWithoutAUTH(t *testing.T) {
	srv := startFakeSMTP(t, []string{"8BITMIME"}, false)
	cfg := srv.cfg()
	cfg.AllowPlaintext = true
	cfg.Username = "user"
	cfg.Password = "pass"

	err := sendViaSMTP(context.Background(), cfg, []string{"a@example.com"}, []byte("Subject: x\r\n\r\nbody\r\n"))
	if err == nil || !strings.Contains(err.Error(), "AUTH") {
		t.Fatalf("sendViaSMTP() error = %v, want one mentioning AUTH", err)
	}
	time.Sleep(50 * time.Millisecond)
	if hasCommand(srv.commands(), "MAIL FROM") {
		t.Errorf("client sent MAIL FROM without authenticating: %v", srv.commands())
	}
}

// runSend runs sendViaSMTP and fails the test if it hasn't returned within
// limit (instead of hanging the test binary).
func runSend(t *testing.T, ctx context.Context, cfg SMTPConfig, limit time.Duration) (time.Duration, error) {
	t.Helper()
	start := time.Now()
	done := make(chan error, 1)
	go func() {
		done <- sendViaSMTP(ctx, cfg, []string{"a@example.com"}, []byte("Subject: x\r\n\r\nbody\r\n"))
	}()
	select {
	case err := <-done:
		return time.Since(start), err
	case <-time.After(limit):
		t.Fatalf("sendViaSMTP() still running after %v", limit)
		return 0, nil
	}
}

func TestSendViaSMTPTimeoutCoversWholeConversation(t *testing.T) {
	old := smtpTimeout
	smtpTimeout = 200 * time.Millisecond
	t.Cleanup(func() { smtpTimeout = old })

	srv := startFakeSMTP(t, nil, true) // greets, then never answers EHLO
	elapsed, err := runSend(t, context.Background(), srv.cfg(), 5*time.Second)
	if err == nil {
		t.Fatal("sendViaSMTP() returned nil against a silent server")
	}
	if elapsed > 2*time.Second {
		t.Errorf("sendViaSMTP() took %v, want about smtpTimeout (200ms)", elapsed)
	}
}

func TestSendViaSMTPStopsOnContextCancel(t *testing.T) {
	srv := startFakeSMTP(t, nil, true)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)

	elapsed, err := runSend(t, ctx, srv.cfg(), 5*time.Second)
	if err == nil {
		t.Fatal("sendViaSMTP() returned nil after ctx was cancelled")
	}
	if elapsed > 2*time.Second {
		t.Errorf("sendViaSMTP() took %v after a 50ms cancel, want prompt return", elapsed)
	}
	if ctx.Err() == nil || !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Errorf("error %v does not say the context was cancelled", err)
	}
}
