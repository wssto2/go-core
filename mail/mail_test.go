package mail_test

import (
	"bufio"
	"context"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/mail"
)

// relay is a minimal SMTP server: it accepts one message per connection and
// keeps what the client sent, without TLS (net/smtp allows PLAIN auth to localhost).
type relay struct {
	addr string
	mu   sync.Mutex
	got  []received
}

type received struct {
	from string
	to   []string
	data string
	auth bool
}

func startRelay(t *testing.T) *relay {
	t.Helper()

	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	r := &relay{addr: ln.Addr().String()}

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}

			go r.serve(c)
		}
	}()

	return r
}

func (r *relay) serve(c net.Conn) {
	defer func() { _ = c.Close() }()

	tp := textproto.NewConn(c)
	_ = tp.PrintfLine("220 relay ready")

	var cur received

	for {
		line, err := tp.ReadLine()
		if err != nil {
			return
		}

		switch cmd := strings.ToUpper(line); {
		case strings.HasPrefix(cmd, "EHLO"):
			_ = tp.PrintfLine("250-relay")
			_ = tp.PrintfLine("250 AUTH PLAIN")
		case strings.HasPrefix(cmd, "AUTH"):
			cur.auth = true
			_ = tp.PrintfLine("235 ok")
		case strings.HasPrefix(cmd, "MAIL FROM:"):
			cur.from = strings.Trim(line[len("MAIL FROM:"):], "<> ")
			_ = tp.PrintfLine("250 ok")
		case strings.HasPrefix(cmd, "RCPT TO:"):
			cur.to = append(cur.to, strings.Trim(line[len("RCPT TO:"):], "<> "))
			_ = tp.PrintfLine("250 ok")
		case cmd == "DATA":
			_ = tp.PrintfLine("354 go")

			body, err := io.ReadAll(tp.DotReader())
			if err != nil {
				return
			}

			cur.data = string(body)

			r.mu.Lock()
			r.got = append(r.got, cur)
			r.mu.Unlock()

			_ = tp.PrintfLine("250 queued")
		case cmd == "QUIT":
			_ = tp.PrintfLine("221 bye")

			return
		default:
			_ = tp.PrintfLine("250 ok")
		}
	}
}

func (r *relay) messages() []received {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]received(nil), r.got...)
}

func TestSMTPSendsTextAndHTMLAsAlternatives(t *testing.T) {
	r := startRelay(t)
	sender := mail.SMTP(mail.SMTPConfig{Addr: r.addr, Username: "u", Password: "p", From: "App <no-reply@example.com>"})

	err := sender.Send(context.Background(), mail.Message{
		To: []string{"Ana <ana@example.com>", "bob@example.com"}, Subject: "Šifra za potvrdu",
		Text: "Vaša šifra je 123456", HTML: "<p>Vaša šifra je <b>123456</b></p>",
	})
	require.NoError(t, err)

	got := r.messages()
	require.Len(t, got, 1)
	require.Equal(t, "no-reply@example.com", got[0].from)
	require.Equal(t, []string{"ana@example.com", "bob@example.com"}, got[0].to)
	require.True(t, got[0].auth)

	head, body, ok := strings.Cut(got[0].data, "\n\n")
	require.True(t, ok)
	require.Contains(t, head, "To: \"Ana\" <ana@example.com>, <bob@example.com>")

	subject := ""

	for _, h := range strings.Split(head, "\n") {
		if v, ok := strings.CutPrefix(h, "Subject: "); ok {
			subject, _ = new(mime.WordDecoder).DecodeHeader(v)
		}
	}

	require.Equal(t, "Šifra za potvrdu", subject)

	mediaType, params, err := mime.ParseMediaType(headerValue(head, "Content-Type"))
	require.NoError(t, err)
	require.Equal(t, "multipart/alternative", mediaType)

	mr := multipart.NewReader(strings.NewReader(body), params["boundary"])

	var parts []string

	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}

		require.NoError(t, err)

		text, err := io.ReadAll(quotedprintable.NewReader(p))
		require.NoError(t, err)

		parts = append(parts, p.Header.Get("Content-Type")+"|"+string(text))
	}

	require.Equal(t, []string{
		"text/plain; charset=UTF-8|Vaša šifra je 123456",
		"text/html; charset=UTF-8|<p>Vaša šifra je <b>123456</b></p>",
	}, parts, "text first, so a client without HTML shows the text")
}

func TestSMTPSendsTextOnlyAsPlainText(t *testing.T) {
	r := startRelay(t)
	require.NoError(t, mail.SMTP(mail.SMTPConfig{Addr: r.addr, From: "no-reply@example.com"}).
		Send(context.Background(), mail.Message{To: []string{"ana@example.com"}, Subject: "Hi", Text: "Hello"}))

	got := r.messages()
	require.Len(t, got, 1)
	require.False(t, got[0].auth, "no credentials, no AUTH")
	require.Contains(t, got[0].data, "Content-Type: text/plain; charset=UTF-8")
}

func TestSendersRefuseMessagesThatCouldInjectHeaders(t *testing.T) {
	r := startRelay(t)
	sender := mail.SMTP(mail.SMTPConfig{Addr: r.addr, From: "no-reply@example.com"})

	for name, m := range map[string]mail.Message{
		"line break in the subject": {To: []string{"a@example.com"}, Subject: "Hi\r\nBcc: x@example.com", Text: "t"},
		"line break in the address": {To: []string{"a@example.com\r\nBcc: x@example.com"}, Subject: "Hi", Text: "t"},
		"not an address":            {To: []string{"ana"}, Subject: "Hi", Text: "t"},
		"no recipient":              {Subject: "Hi", Text: "t"},
		"no text":                   {To: []string{"a@example.com"}, Subject: "Hi"},
	} {
		require.Error(t, sender.Send(context.Background(), m), name)
		require.Error(t, mail.NewSink().Send(context.Background(), m), name+" (sink)")
	}

	require.Empty(t, r.messages())
}

func TestSMTPNamesAWrongConfiguration(t *testing.T) {
	m := mail.Message{To: []string{"a@example.com"}, Subject: "s", Text: "t"}

	require.ErrorContains(t, mail.SMTP(mail.SMTPConfig{Addr: "smtp.example.com", From: "a@example.com"}).Send(context.Background(), m), "host:port")
	require.ErrorContains(t, mail.SMTP(mail.SMTPConfig{Addr: "smtp.example.com:25", From: "nobody"}).Send(context.Background(), m), "SMTPConfig.From")
}

func TestSMTPGivesUpWhenTheRelayIsSilent(t *testing.T) {
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}

			go func() { _, _ = bufio.NewReader(c).ReadByte() }() // never greets
		}
	}()

	sender := mail.SMTP(mail.SMTPConfig{Addr: ln.Addr().String(), From: "a@example.com", Timeout: 200 * time.Millisecond})

	start := time.Now()
	err = sender.Send(context.Background(), mail.Message{To: []string{"a@example.com"}, Subject: "s", Text: "t"})
	require.Error(t, err)
	require.Less(t, time.Since(start), 3*time.Second)
}

func TestSinkRecordsACopyInOrder(t *testing.T) {
	sink := mail.NewSink()
	_, found := sink.Last()
	require.False(t, found)

	to := []string{"ana@example.com"}
	require.NoError(t, sink.Send(context.Background(), mail.Message{To: to, Subject: "one", Text: "1"}))
	require.NoError(t, sink.Send(context.Background(), mail.Message{To: []string{"Bob <bob@example.com>"}, Subject: "two", Text: "2"}))

	to[0] = "changed@example.com"

	require.Equal(t, "ana@example.com", sink.Sent()[0].To[0], "the sink keeps its own copy")
	require.Len(t, sink.To("BOB@example.com"), 1, "found by address, without case or name")

	last, _ := sink.Last()
	require.Equal(t, "two", last.Subject)

	sink.Reset()
	require.Empty(t, sink.Sent())
}

func TestTemplatesPickTheLocaleThenTheLanguageThenEnglish(t *testing.T) {
	files := fstest.MapFS{
		"en/hello.subject.txt":  {Data: []byte("Hello {{.Name}}\n")},
		"en/hello.txt":          {Data: []byte("Hi {{.Name}}")},
		"en/hello.html":         {Data: []byte("<p>Hi {{.Name}}</p>")},
		"pt/hello.subject.txt":  {Data: []byte("Olá {{.Name}}")},
		"pt/hello.txt":          {Data: []byte("Oi {{.Name}}")},
		"en/plain.subject.txt":  {Data: []byte("Plain")},
		"en/plain.txt":          {Data: []byte("only text")},
		"en/broken.subject.txt": {Data: []byte("no body")},
	}
	r := mail.Templates(files)
	data := map[string]string{"Name": "<Ana>"}

	for locale, want := range map[string]string{"en": "Hello <Ana>", "pt-BR": "Olá <Ana>", "pt": "Olá <Ana>", "hr": "Hello <Ana>", "": "Hello <Ana>"} {
		c, err := r.Render(context.Background(), locale, "hello", data)
		require.NoError(t, err, locale)
		require.Equal(t, want, c.Subject, locale)
	}

	c, err := r.Render(context.Background(), "en", "hello", data)
	require.NoError(t, err)
	require.Equal(t, "<p>Hi &lt;Ana&gt;</p>", c.HTML, "the HTML template escapes")
	require.Equal(t, "Hi <Ana>", c.Text, "the text template does not")

	plain, err := r.Render(context.Background(), "en", "plain", nil)
	require.NoError(t, err)
	require.Empty(t, plain.HTML)

	_, err = r.Render(context.Background(), "en", "missing", nil)
	require.ErrorIs(t, err, mail.ErrNoTemplate)

	_, err = r.Render(context.Background(), "en", "broken", nil)
	require.ErrorContains(t, err, "plain-text body")

	_, err = r.Render(context.Background(), "en", "hello", map[string]string{})
	require.Error(t, err, "a field the template reads but the data lacks is an error, not an empty string")
}

func headerValue(head, name string) string {
	for _, line := range strings.Split(head, "\n") {
		if v, ok := strings.CutPrefix(line, name+": "); ok {
			return v
		}
	}

	return ""
}
