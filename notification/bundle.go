package notification

import (
	"fmt"
	"strings"
)

// defaultBundleText is the text of a bundled push in English, Croatian, Bosnian, Serbian and Slovenian: "3 nove
// obavijesti", with the newest titles as the body. Another language is English; PushBundleText replaces all of it.
func defaultBundleText(locale string, count int, titles []string) (title, body string) {
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

// slavic picks the plural form: 2 to 4, except 12 to 14, take few; the rest take many.
func slavic(count int, few, many string) string {
	if last := count % 10; last >= 2 && last <= 4 && (count%100 < 12 || count%100 > 14) {
		return few
	}

	return many
}

// slovenian has a dual: 2 is "novi obvestili", 3 and 4 "nova obvestila", the rest "novih obvestil".
func slovenian(count int) string {
	switch count % 100 {
	case 2:
		return "novi obvestili"
	case 3, 4:
		return "nova obvestila"
	default:
		return "novih obvestil"
	}
}
