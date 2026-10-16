//nolint:exhaustruct,exhaustruct_v5,funlen,gochecknoglobals,goconst,lll,paralleltest,varnamelen // tests in the style of the package's others
package mail_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	stdmail "net/mail"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/mail"
)

var (
	boundaryRE = regexp.MustCompile(`[0-9a-f]{60}`)
	dateRE     = regexp.MustCompile(`(?m)^Date: .*$`)
)

// sendThrough sends m to a test relay and returns what arrived, with the Date and the random
// boundaries replaced by names (B1, B2 in order of appearance) so the whole message can be compared.
func sendThrough(t *testing.T, m mail.Message) (string, string) {
	t.Helper()

	r := startRelay(t)
	require.NoError(t, mail.SMTP(mail.SMTPConfig{Addr: r.addr, From: "App <no-reply@example.com>"}).Send(context.Background(), m))

	got := r.messages()
	require.Len(t, got, 1)

	names := map[string]string{}
	norm := boundaryRE.ReplaceAllStringFunc(got[0].data, func(b string) string {
		if _, ok := names[b]; !ok {
			names[b] = "B" + strconv.Itoa(len(names)+1)
		}

		return names[b]
	})

	return got[0].data, dateRE.ReplaceAllString(norm, "Date: DATE")
}

var png70 = bytes.Repeat([]byte{0x89}, 70) // 70 bytes: 96 base64 characters, so the part wraps

func TestMIMEGoldenTextOnly(t *testing.T) {
	_, got := sendThrough(t, mail.Message{To: []string{"ana@example.com"}, Subject: "Hi", Text: "Hello"})

	require.Equal(t, "From: \"App\" <no-reply@example.com>\n"+
		"To: <ana@example.com>\n"+
		"Subject: Hi\n"+
		"Date: DATE\n"+
		"MIME-Version: 1.0\n"+
		"Content-Type: text/plain; charset=UTF-8\n"+
		"Content-Transfer-Encoding: quoted-printable\n"+
		"\n"+
		"Hello\n", got)
}

// Without inline parts the message is what it was before Inline existed.
func TestMIMEGoldenHTMLWithoutInline(t *testing.T) {
	_, got := sendThrough(t, mail.Message{To: []string{"ana@example.com"}, Subject: "Hi", Text: "Hello", HTML: "<p>Hello</p>"})

	require.Equal(t, "From: \"App\" <no-reply@example.com>\n"+
		"To: <ana@example.com>\n"+
		"Subject: Hi\n"+
		"Date: DATE\n"+
		"MIME-Version: 1.0\n"+
		"Content-Type: multipart/alternative; boundary=B1\n"+
		"\n"+
		"--B1\n"+
		"Content-Transfer-Encoding: quoted-printable\n"+
		"Content-Type: text/plain; charset=UTF-8\n"+
		"\n"+
		"Hello\n"+
		"--B1\n"+
		"Content-Transfer-Encoding: quoted-printable\n"+
		"Content-Type: text/html; charset=UTF-8\n"+
		"\n"+
		"<p>Hello</p>\n"+
		"--B1--\n", got)
}

func TestMIMEGoldenHTMLWithInline(t *testing.T) {
	_, got := sendThrough(t, mail.Message{
		To: []string{"ana@example.com"}, Subject: "Hi", Text: "Hello", HTML: `<img src="cid:logo@example.com">`,
		Inline: []mail.Inline{{CID: "logo@example.com", ContentType: "image/png", Name: "logo.png", Data: png70}},
	})

	b64 := base64.StdEncoding.EncodeToString(png70)

	require.Equal(t, "From: \"App\" <no-reply@example.com>\n"+
		"To: <ana@example.com>\n"+
		"Subject: Hi\n"+
		"Date: DATE\n"+
		"MIME-Version: 1.0\n"+
		"Content-Type: multipart/alternative; boundary=B1\n"+
		"\n"+
		"--B1\n"+
		"Content-Transfer-Encoding: quoted-printable\n"+
		"Content-Type: text/plain; charset=UTF-8\n"+
		"\n"+
		"Hello\n"+
		"--B1\n"+
		"Content-Type: multipart/related; boundary=B2; type=\"text/html\"\n"+
		"\n"+
		"--B2\n"+
		"Content-Transfer-Encoding: quoted-printable\n"+
		"Content-Type: text/html; charset=UTF-8\n"+
		"\n"+
		"<img src=3D\"cid:logo@example.com\">\n"+
		"--B2\n"+
		"Content-Disposition: inline; filename=logo.png\n"+
		"Content-ID: <logo@example.com>\n"+
		"Content-Transfer-Encoding: base64\n"+
		"Content-Type: image/png; name=logo.png\n"+
		"\n"+
		b64[:76]+"\n"+b64[76:]+"\n"+
		"--B2--\n"+
		"--B1--\n", got)
}

// The output parsed the way a mail client does: alternative, text first, then related with the HTML and the image.
func TestMIMEStructureParses(t *testing.T) {
	raw, _ := sendThrough(t, mail.Message{
		To: []string{"ana@example.com"}, Subject: "Grüße", Text: "Hello Ana", HTML: `<img src="cid:a@x"><img src="cid:b@x">`,
		Inline: []mail.Inline{
			{CID: "a@x", ContentType: "image/png", Name: "a.png", Data: png70},
			{CID: "b@x", ContentType: "image/gif", Data: []byte("GIF89a")},
		},
	})

	msg, err := stdmail.ReadMessage(strings.NewReader(raw))
	require.NoError(t, err)

	mt, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	require.NoError(t, err)
	require.Equal(t, "multipart/alternative", mt)

	alt := multipart.NewReader(msg.Body, params["boundary"])

	text, err := alt.NextPart()
	require.NoError(t, err)
	require.Equal(t, "text/plain; charset=UTF-8", text.Header.Get("Content-Type"))

	body, err := io.ReadAll(quotedprintable.NewReader(text))
	require.NoError(t, err)
	require.Equal(t, "Hello Ana", string(body))

	related, err := alt.NextPart()
	require.NoError(t, err)

	mt, params, err = mime.ParseMediaType(related.Header.Get("Content-Type"))
	require.NoError(t, err)
	require.Equal(t, "multipart/related", mt)
	require.Equal(t, "text/html", params["type"])

	rel := multipart.NewReader(related, params["boundary"])

	html, err := rel.NextPart()
	require.NoError(t, err)
	require.Equal(t, "text/html; charset=UTF-8", html.Header.Get("Content-Type"))

	body, err = io.ReadAll(quotedprintable.NewReader(html))
	require.NoError(t, err)
	require.Equal(t, `<img src="cid:a@x"><img src="cid:b@x">`, string(body))

	for _, want := range []struct {
		cid, kind string
		data      []byte
	}{{"<a@x>", "image/png", png70}, {"<b@x>", "image/gif", []byte("GIF89a")}} {
		p, err := rel.NextPart()
		require.NoError(t, err)
		require.Equal(t, want.cid, p.Header.Get("Content-ID"))
		require.Equal(t, "base64", p.Header.Get("Content-Transfer-Encoding"))

		kind, _, err := mime.ParseMediaType(p.Header.Get("Content-Type"))
		require.NoError(t, err)
		require.Equal(t, want.kind, kind)

		disp, _, err := mime.ParseMediaType(p.Header.Get("Content-Disposition"))
		require.NoError(t, err)
		require.Equal(t, "inline", disp)

		data, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, p))
		require.NoError(t, err)
		require.Equal(t, want.data, data)
	}

	_, err = rel.NextPart()
	require.ErrorIs(t, err, io.EOF)

	_, err = alt.NextPart()
	require.ErrorIs(t, err, io.EOF, "the related part is the last of the alternative")
}

func TestInlineIsCheckedLikeTheRestOfTheMessage(t *testing.T) {
	good := mail.Inline{CID: "logo@example.com", ContentType: "image/png", Data: []byte("x")}
	base := mail.Message{To: []string{"a@example.com"}, Subject: "s", Text: "t", HTML: "<p>h</p>"}

	with := func(mod func(*mail.Inline)) mail.Message {
		in := good
		mod(&in)

		m := base
		m.Inline = []mail.Inline{in}

		return m
	}

	require.NoError(t, with(func(*mail.Inline) {}).Check())

	noHTML := with(func(*mail.Inline) {})
	noHTML.HTML = ""

	twice := base
	twice.Inline = []mail.Inline{good, good}

	r := startRelay(t)
	sender := mail.SMTP(mail.SMTPConfig{Addr: r.addr, From: "no-reply@example.com"})

	for name, m := range map[string]mail.Message{
		"header injection in the CID":      with(func(i *mail.Inline) { i.CID = "x@example.com\r\nBcc: evil@example.com" }),
		"a line feed in the CID":           with(func(i *mail.Inline) { i.CID = "x\nBcc: evil@example.com" }),
		"a closing bracket in the CID":     with(func(i *mail.Inline) { i.CID = "x>\r\nX: y" }),
		"a space in the CID":               with(func(i *mail.Inline) { i.CID = "my logo" }),
		"no CID":                           with(func(i *mail.Inline) { i.CID = "" }),
		"no content type":                  with(func(i *mail.Inline) { i.ContentType = "" }),
		"header injection in content type": with(func(i *mail.Inline) { i.ContentType = "image/png\r\nBcc: evil@example.com" }),
		"a line break in the name":         with(func(i *mail.Inline) { i.Name = "a.png\r\nBcc: evil@example.com" }),
		"no data":                          with(func(i *mail.Inline) { i.Data = nil }),
		"inline parts without HTML":        noHTML,
		"the same CID twice":               twice,
	} {
		require.ErrorIs(t, m.Check(), mail.ErrPermanent, name)
		require.ErrorIs(t, sender.Send(context.Background(), m), mail.ErrPermanent, name)
		require.Error(t, mail.NewSink().Send(context.Background(), m), name+" (sink)")
	}

	require.Empty(t, r.messages(), "nothing reached the relay")
}

func TestSinkRecordsInlineParts(t *testing.T) {
	sink := mail.NewSink()
	data := []byte("PNG")
	m := mail.Message{
		To: []string{"a@example.com"}, Subject: "s", Text: "t", HTML: `<img src="cid:l@x">`,
		Inline: []mail.Inline{{CID: "l@x", ContentType: "image/png", Data: data}},
	}

	require.NoError(t, sink.Send(context.Background(), m))

	m.Inline[0].CID = "changed"
	data[0] = 'X'

	last, ok := sink.Last()
	require.True(t, ok)
	require.Equal(t, []mail.Inline{{CID: "l@x", ContentType: "image/png", Data: []byte("PNG")}}, last.Inline, "a copy: the caller's later changes do not reach it")
}
