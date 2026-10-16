//nolint:exhaustruct,exhaustruct_v5,goconst,paralleltest,varnamelen // tests in the style of the package's others
package mail_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/mail"
)

func TestRedirectSendsOnceToTheOneAddress(t *testing.T) {
	sink := mail.NewSink()
	inline := []mail.Inline{{CID: "logo", ContentType: "image/png", Data: []byte{1}}}

	err := mail.Redirect(sink, "dev@example.com").Send(context.Background(), mail.Message{
		To:      []string{"a@x.hr", "Bo <b@y.hr>"},
		Subject: "Hello",
		Text:    "text",
		HTML:    "<img src=\"cid:logo\">",
		Inline:  inline,
	})
	require.NoError(t, err)

	sent := sink.Sent()
	require.Len(t, sent, 1)
	require.Equal(t, []string{"dev@example.com"}, sent[0].To)
	require.Equal(t, "[to a@x.hr, Bo <b@y.hr>] Hello", sent[0].Subject)
	require.Equal(t, "text", sent[0].Text)
	require.Equal(t, "<img src=\"cid:logo\">", sent[0].HTML)
	require.Equal(t, inline, sent[0].Inline)
}

func TestRedirectRefusesABadAddress(t *testing.T) {
	for _, to := range []string{"", "not an address", "a@x.hr\r\nBcc: b@y.hr"} {
		sink := mail.NewSink()

		err := mail.Redirect(sink, to).Send(context.Background(), mail.Message{To: []string{"real@x.hr"}, Subject: "s", Text: "t"})
		require.ErrorContains(t, err, "Redirect address", to)
		require.Empty(t, sink.Sent(), to)
	}
}

func TestRedirectKeepsTheErrorOfTheSender(t *testing.T) {
	next := mail.SenderFunc(func(context.Context, mail.Message) error { return mail.ErrPermanent })

	err := mail.Redirect(next, "dev@example.com").Send(context.Background(), mail.Message{To: []string{"a@x.hr"}, Text: "t"})
	require.ErrorIs(t, err, mail.ErrPermanent)
}
