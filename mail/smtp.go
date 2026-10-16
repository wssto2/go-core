package mail

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
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
			refused := fmt.Errorf("mail: the relay refused the recipient %s: %w", addr.Address, err)

			var answer *textproto.Error
			if errors.As(err, &answer) && answer.Code >= 500 {
				return permanent{refused} // a 5xx for the address: it will not be accepted later either
			}

			return refused
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
// is multipart/alternative with the text first; with inline parts the HTML and
// those parts are a multipart/related part of it.
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

	err := alternatives(mw, m)
	if err != nil {
		return nil, err
	}

	err = mw.Close()
	if err != nil {
		return nil, fmt.Errorf("mail: closing the message: %w", err)
	}

	b.WriteString("Content-Type: multipart/alternative; boundary=" + mw.Boundary() + "\r\n\r\n")
	b.WriteString(parts.String())

	return []byte(b.String()), nil
}

// alternatives writes the text, then the HTML: alone, or with its inline parts as a multipart/related part.
func alternatives(parts *multipart.Writer, msg Message) error {
	err := textPart(parts, "text/plain", msg.Text)
	if err != nil {
		return err
	}

	if len(msg.Inline) == 0 {
		return textPart(parts, "text/html", msg.HTML)
	}

	return relatedPart(parts, msg)
}

// textPart adds a quoted-printable UTF-8 part.
func textPart(mw *multipart.Writer, kind, body string) error {
	header := textproto.MIMEHeader{}
	header.Set("Content-Type", kind+"; charset=UTF-8")
	header.Set("Content-Transfer-Encoding", "quoted-printable")

	w, err := mw.CreatePart(header)
	if err != nil {
		return fmt.Errorf("mail: adding the %s part: %w", kind, err)
	}

	return encode(w, body)
}

// relatedPart adds the multipart/related part of the alternative: the HTML first, then each
// inline part, so a "cid:" in the HTML finds its part in the same container.
func relatedPart(alt *multipart.Writer, msg Message) error {
	var buf strings.Builder

	related := multipart.NewWriter(&buf)

	err := textPart(related, "text/html", msg.HTML)
	if err != nil {
		return err
	}

	for _, part := range msg.Inline {
		err = inlinePart(related, part)
		if err != nil {
			return err
		}
	}

	err = related.Close()
	if err != nil {
		return fmt.Errorf("mail: closing the related part: %w", err)
	}

	header := textproto.MIMEHeader{}
	header.Set("Content-Type", mime.FormatMediaType("multipart/related",
		map[string]string{"type": "text/html", "boundary": related.Boundary()}))

	body, err := alt.CreatePart(header)
	if err != nil {
		return fmt.Errorf("mail: adding the related part: %w", err)
	}

	// The writer of the alternative adds the line break before its next boundary.
	_, err = io.WriteString(body, strings.TrimSuffix(buf.String(), "\r\n"))
	if err != nil {
		return fmt.Errorf("mail: writing the related part: %w", err)
	}

	return nil
}

func inlinePart(related *multipart.Writer, part Inline) error {
	mediaType, _, _ := mime.ParseMediaType(part.ContentType) // Check parsed it

	header := textproto.MIMEHeader{}
	header.Set("Content-Type", mime.FormatMediaType(mediaType, nameParam("name", part.Name)))
	header.Set("Content-Transfer-Encoding", "base64")
	header["Content-ID"] = []string{"<" + part.CID + ">"} // by key: Set would write it as Content-Id
	header.Set("Content-Disposition", mime.FormatMediaType("inline", nameParam("filename", part.Name)))

	w, err := related.CreatePart(header)
	if err != nil {
		return fmt.Errorf("mail: adding the inline part %q: %w", part.CID, err)
	}

	_, err = io.WriteString(w, wrap76(base64.StdEncoding.EncodeToString(part.Data)))
	if err != nil {
		return fmt.Errorf("mail: writing the inline part %q: %w", part.CID, err)
	}

	return nil
}

func nameParam(key, name string) map[string]string {
	if name == "" {
		return nil
	}

	return map[string]string{key: name}
}

// wrap76 breaks base64 into the lines of at most 76 characters RFC 2045 asks for.
func wrap76(text string) string {
	var out strings.Builder

	for len(text) > 76 {
		out.WriteString(text[:76] + "\r\n")
		text = text[76:]
	}

	return out.String() + text
}

func encode(w io.Writer, body string) error {
	qp := quotedprintable.NewWriter(w)

	if _, err := qp.Write([]byte(body)); err != nil {
		return err
	}

	return qp.Close()
}
