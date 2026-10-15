// Package mailtext is the wording of the incident alerts, in English, Croatian, Bosnian and Slovenian. An alert is a
// notification (category "incident.alert"): its title is the e-mail's subject and its body the e-mail's text, which
// notification renders, so what this package writes is the notification's Title and Body for one recipient, in
// their language. The language is the person's (Recipient.Locale), then the language of a locale such as "hr-HR",
// then English.
package mailtext

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Alert is a new incident, as the alert words it.
type Alert struct {
	PublicCode string
	Title      string
	// Severity is "low", "medium", "high" or "critical"; Source "user_report", "runtime_error", "client_error" or
	// "mixed". Another value is written as it is.
	Severity string
	Source   string
}

// maxTitle and maxListed bound the digest's body, which a notification cuts at 500 characters.
const (
	maxTitle  = 60
	maxListed = 8
)

type words struct {
	opened   string            // "New %s incident: %s", the severity and the title
	digest   string            // "New incidents: %d"
	code     string            // "Code"
	source   string            // "Source"
	more     string            // "and %d more"
	severity map[string]string // by severity, in the masculine
	sourceOf map[string]string
}

var languages = map[string]words{ //nolint:gochecknoglobals // fixed text
	"en": {
		opened: "New %s incident: %s", digest: "New incidents: %d", code: "Code", source: "Source", more: "and %d more",
		severity: map[string]string{"low": "low", "medium": "medium", "high": "high", "critical": "critical"},
		sourceOf: map[string]string{
			"user_report": "a person's report", "runtime_error": "server error", "client_error": "browser error",
			"mixed": "several sources",
		},
	},
	"hr": {
		opened: "Novi %s incident: %s", digest: "Novi incidenti: %d", code: "Šifra", source: "Izvor", more: "i još %d",
		severity: map[string]string{"low": "nizak", "medium": "srednji", "high": "visok", "critical": "kritičan"},
		sourceOf: map[string]string{
			"user_report": "prijava korisnika", "runtime_error": "greška poslužitelja", "client_error": "greška preglednika",
			"mixed": "više izvora",
		},
	},
	"bs": {
		opened: "Novi %s incident: %s", digest: "Novi incidenti: %d", code: "Šifra", source: "Izvor", more: "i još %d",
		severity: map[string]string{"low": "nizak", "medium": "srednji", "high": "visok", "critical": "kritičan"},
		sourceOf: map[string]string{
			"user_report": "prijava korisnika", "runtime_error": "greška servera", "client_error": "greška preglednika",
			"mixed": "više izvora",
		},
	},
	"sl": {
		opened: "Nov %s incident: %s", digest: "Novi incidenti: %d", code: "Koda", source: "Vir", more: "in še %d",
		severity: map[string]string{"low": "nizek", "medium": "srednji", "high": "visok", "critical": "kritičen"},
		sourceOf: map[string]string{
			"user_report": "prijava uporabnika", "runtime_error": "napaka strežnika", "client_error": "napaka brskalnika",
			"mixed": "več virov",
		},
	},
}

// wordsOf is the language of the locale: "hr", "hr-HR" and "hr_HR" are Croatian, an unknown language English.
func wordsOf(locale string) words {
	lang, _, _ := strings.Cut(strings.ReplaceAll(strings.ToLower(locale), "_", "-"), "-")
	if w, ok := languages[lang]; ok {
		return w
	}

	return languages["en"]
}

func (w words) severityOf(severity string) string {
	if name, ok := w.severity[severity]; ok {
		return name
	}

	return severity
}

func (w words) sourceName(source string) string {
	if name, ok := w.sourceOf[source]; ok {
		return name
	}

	return source
}

// Opened is the title and body of the alert of one new incident: "New critical incident: Title" and the incident's code
// and source.
func Opened(locale string, alert Alert) (string, string) {
	w := wordsOf(locale)

	return fmt.Sprintf(w.opened, w.severityOf(alert.Severity), alert.Title),
		fmt.Sprintf("%s: %s · %s: %s", w.code, alert.PublicCode, w.source, w.sourceName(alert.Source))
}

// Digest is the title and body of the alert of the incidents that waited for the digest: how many, and a line each
// (severity, title, code) for the first few.
func Digest(locale string, alerts []Alert) (string, string) {
	w := wordsOf(locale)

	var body strings.Builder

	for i, alert := range alerts {
		if i == maxListed {
			fmt.Fprintf(&body, "… %s", fmt.Sprintf(w.more, len(alerts)-maxListed))

			break
		}

		fmt.Fprintf(&body, "- [%s] %s (%s)\n", strings.ToUpper(alert.Severity), shorten(alert.Title), alert.PublicCode)
	}

	return fmt.Sprintf(w.digest, len(alerts)), strings.TrimSpace(body.String())
}

// shorten cuts a title to maxTitle characters.
func shorten(title string) string {
	if utf8.RuneCountInString(title) <= maxTitle {
		return title
	}

	return string([]rune(title)[:maxTitle-1]) + "…"
}
