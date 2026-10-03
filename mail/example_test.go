package mail_test

import (
	"context"
	"fmt"
	"testing/fstest"

	"github.com/wssto2/go-core/mail"
)

func ExampleSink() {
	sink := mail.NewSink()

	_ = sink.Send(context.Background(), mail.Message{
		To: []string{"ana@example.com"}, Subject: "Your code", Text: "Your code is 123456",
	})

	last, _ := sink.Last()
	fmt.Println(last.To[0], "|", last.Subject, "|", last.Text)
	// Output: ana@example.com | Your code | Your code is 123456
}

func ExampleSMTP() {
	sender := mail.SMTP(mail.SMTPConfig{
		Addr: "smtp.example.com:587", Username: "apikey", Password: "secret",
		From: "App <no-reply@example.com>",
	})

	_ = sender // pass it to identity.WithMail(sender)

	fmt.Println("ready")
	// Output: ready
}

func ExampleTemplates() {
	files := fstest.MapFS{
		"en/welcome.subject.txt": {Data: []byte("Welcome, {{.Name}}")},
		"en/welcome.txt":         {Data: []byte("Hi {{.Name}}, your account is ready.")},
		"hr/welcome.subject.txt": {Data: []byte("Dobrodošli, {{.Name}}")},
		"hr/welcome.txt":         {Data: []byte("Bok {{.Name}}, vaš račun je spreman.")},
	}
	render := mail.Templates(files)

	data := map[string]string{"Name": "Ana"}
	for _, locale := range []string{"hr", "de"} { // no German: English is the fallback
		c, _ := render.Render(context.Background(), locale, "welcome", data)
		fmt.Println(c.Subject)
	}
	// Output:
	// Dobrodošli, Ana
	// Welcome, Ana
}

func ExampleContent_To() {
	c := mail.Content{Subject: "Hello", Text: "Hi"}
	m := c.To("ana@example.com")

	fmt.Println(m.To, m.Subject, m.Check())
	// Output: [ana@example.com] Hello <nil>
}

func ExampleSenderFunc() {
	var sender mail.Sender = mail.SenderFunc(func(_ context.Context, m mail.Message) error {
		fmt.Println("would send", m.Subject)

		return nil
	})

	_ = sender.Send(context.Background(), mail.Message{To: []string{"a@example.com"}, Subject: "Hi", Text: "t"})
	// Output: would send Hi
}

func ExampleRendererFunc() {
	var render mail.Renderer = mail.RendererFunc(func(_ context.Context, locale string, name mail.Name, _ any) (mail.Content, error) {
		return mail.Content{Subject: string(name) + " in " + locale, Text: "…"}, nil
	})

	c, _ := render.Render(context.Background(), "hr", "welcome", nil)
	fmt.Println(c.Subject)
	// Output: welcome in hr
}

func ExampleFallback() {
	mine := mail.RendererFunc(func(_ context.Context, _ string, name mail.Name, _ any) (mail.Content, error) {
		if name == "welcome" {
			return mail.Content{Subject: "Welcome to us", Text: "…"}, nil
		}

		return mail.Content{}, mail.ErrNoTemplate
	})
	defaults := mail.Templates(fstest.MapFS{
		"en/goodbye.subject.txt": {Data: []byte("Goodbye")}, "en/goodbye.txt": {Data: []byte("…")},
	})
	render := mail.Fallback(mine, defaults)

	for _, name := range []mail.Name{"welcome", "goodbye"} {
		c, _ := render.Render(context.Background(), "en", name, nil)
		fmt.Println(c.Subject)
	}
	// Output:
	// Welcome to us
	// Goodbye
}
