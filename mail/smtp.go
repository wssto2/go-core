package mail

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strings"
	"time"
)

// SMTPConfig is how to reach a relay.
type SMTPConfig struct {
	// Addr is "host:port", for example "smtp.example.com:587".
	Addr string
	// Username and Password sign in to the relay; leave both empty for none.
	Username, Password string
	// From is the sender, "no-reply@example.com" or "App <no-reply@example.com>".
	From string
	// Implicit connects with TLS from the first byte (port 465). Without it
	// the connection starts in the clear and is upgraded with STARTTLS when
	// the relay offers it, which is what port 587 does.
	Implicit bool
	// Timeout bounds the whole conversation. Default 15 seconds.
	Timeout time.Duration
}

type smtpSender struct {
	cfg  SMTPConfig
	from *mail.Address
	host string
}

// SMTP returns a Sender over a relay, built on net/smtp. A configuration that
// cannot work (no host, a From that is not an address) is reported by every
// Send, naming what to set.
func SMTP(cfg SMTPConfig) Sender {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 15 * time.Second
	}

	host, _, err := net.SplitHostPort(cfg.Addr)
	if err != nil || host == "" {
		return failing{fmt.Errorf("mail: SMTPConfig.Addr %q is not host:port: set it like \"smtp.example.com:587\"", cfg.Addr)}
	}

	from, err := mail.ParseAddress(cfg.From)
	if err != nil {
		return failing{fmt.Errorf("mail: SMTPConfig.From %q is not an e-mail address: set it like \"App <no-reply@example.com>\"", cfg.From)}
	}

	return &smtpSender{cfg: cfg, from: from, host: host}
}

type failing struct{ err error }

func (f failing) Send(context.Context, Message) error { return f.err }

func (s *smtpSender) Send(ctx context.Context, m Message) error {
	if err := m.Check(); err != nil {
		return err
	}

	body, err := s.render(m)
	if err != nil {
		return err
	}

	dialer := net.Dialer{Timeout: s.cfg.Timeout}

	var conn net.Conn
	if s.cfg.Implicit {
		conn, err = (&tls.Dialer{NetDialer: &dialer, Config: &tls.Config{ServerName: s.host, MinVersion: tls.VersionTLS12}}).DialContext(ctx, "tcp", s.cfg.Addr)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", s.cfg.Addr)
	}

	if err != nil {
		return fmt.Errorf("mail: cannot reach %s: %w", s.cfg.Addr, err)
	}

	defer func() { _ = conn.Close() }()

	// One deadline for the whole conversation, so a stalled relay cannot hold the caller.
	deadline := time.Now().Add(s.cfg.Timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}

	if err := conn.SetDeadline(deadline); err != nil {
		return fmt.Errorf("mail: %w", err)
	}

	client, err := smtp.NewClient(conn, s.host)
	if err != nil {
		return fmt.Errorf("mail: the relay did not greet: %w", err)
	}

	defer func() { _ = client.Close() }()

	if !s.cfg.Implicit {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: s.host, MinVersion: tls.VersionTLS12}); err != nil {
				return fmt.Errorf("mail: STARTTLS failed: %w", err)
			}
		}
	}

	if s.cfg.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.host)); err != nil {
			return fmt.Errorf("mail: the relay refused the credentials: %w", err)
		}
	}

	if err := client.Mail(s.from.Address); err != nil {
		return fmt.Errorf("mail: the relay refused the sender: %w", err)
	}

	for _, to := range m.To {
		addr, _ := mail.ParseAddress(to) // Check parsed it

		if err := client.Rcpt(addr.Address); err != nil {
			return fmt.Errorf("mail: the relay refused the recipient %s: %w", addr.Address, err)
		}
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("mail: DATA failed: %w", err)
	}

	if _, err := w.Write(body); err != nil {
		return fmt.Errorf("mail: writing the message failed: %w", err)
	}

	if err := w.Close(); err != nil {
		return fmt.Errorf("mail: the relay did not accept the message: %w", err)
	}

	return client.Quit()
}

// render is the RFC 5322 message: text only is a text/plain body; with HTML it
// is multipart/alternative with the text first.
func (s *smtpSender) render(m Message) ([]byte, error) {
	var b strings.Builder

	to := make([]string, len(m.To))
	for i, t := range m.To {
		addr, _ := mail.ParseAddress(t)
		to[i] = addr.String()
	}

	b.WriteString("From: " + s.from.String() + "\r\n")
	b.WriteString("To: " + strings.Join(to, ", ") + "\r\n")
	b.WriteString("Subject: " + mime.QEncoding.Encode("UTF-8", m.Subject) + "\r\n")
	b.WriteString("Date: " + time.Now().UTC().Format(time.RFC1123Z) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")

	if m.HTML == "" {
		b.WriteString("Content-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n")

		if err := encode(&b, m.Text); err != nil {
			return nil, err
		}

		return []byte(b.String()), nil
	}

	var parts strings.Builder

	mw := multipart.NewWriter(&parts)

	for _, p := range []struct{ kind, body string }{{"text/plain", m.Text}, {"text/html", m.HTML}} {
		h := textproto.MIMEHeader{}
		h.Set("Content-Type", p.kind+"; charset=UTF-8")
		h.Set("Content-Transfer-Encoding", "quoted-printable")

		w, err := mw.CreatePart(h)
		if err != nil {
			return nil, err
		}

		if err := encode(w, p.body); err != nil {
			return nil, err
		}
	}

	if err := mw.Close(); err != nil {
		return nil, err
	}

	b.WriteString("Content-Type: multipart/alternative; boundary=" + mw.Boundary() + "\r\n\r\n")
	b.WriteString(parts.String())

	return []byte(b.String()), nil
}

func encode(w io.Writer, body string) error {
	qp := quotedprintable.NewWriter(w)

	if _, err := qp.Write([]byte(body)); err != nil {
		return err
	}

	return qp.Close()
}
