package notification

import (
	"fmt"
	"strings"
)

// defaultBundleText is the text of a bundled push in English, Croatian, Bosnian, Serbian and Slovenian: "3 nove
// obavijesti", with the newest titles as the body. Another language is English; PushBundleText replaces all of it.
func defaultBundleText(locale string, count int, titles []string) (string, string) {
	shown := titles
	if len(shown) > bundleTitlesShown {
		shown = shown[len(shown)-bundleTitlesShown:]
	}

	lang, _, _ := strings.Cut(strings.ReplaceAll(strings.ToLower(locale), "_", "-"), "-")

	var phrase string

	switch lang {
	case "hr":
		phrase = slavic(count, "nove obavijesti", "novih obavijesti")
	case "bs":
		phrase = slavic(count, "nova obavještenja", "novih obavještenja")
	case "sr":
		phrase = slavic(count, "nova obaveštenja", "novih obaveštenja")
	case "sl":
		phrase = slovenian(count)
	default:
		phrase = "new notifications"
	}

	return fmt.Sprintf("%d %s", count, phrase), strings.Join(shown, " · ")
}

// The counts the plural forms turn on.
const (
	decade  = 10
	century = 100
	dual    = 2
	few     = 4
	teens   = 12 // 12 to 14 take the plural of many
)

// slavic picks the plural form: 2 to 4, except 12 to 14, take few; the rest take many.
func slavic(count int, fewForm, manyForm string) string {
	last := count % decade
	if last >= dual && last <= few && (count%century < teens || count%century > teens+2) {
		return fewForm
	}

	return manyForm
}

// slovenian has a dual: 2 is "novi obvestili", 3 and 4 "nova obvestila", the rest "novih obvestil".
func slovenian(count int) string {
	switch count % century {
	case dual:
		return "novi obvestili"
	case dual + 1, few:
		return "nova obvestila"
	default:
		return "novih obvestil"
	}
}
