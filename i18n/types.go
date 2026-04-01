package i18n

// I18n represents a map for internationalized strings.
type I18n map[string]string

type Config struct {
	FallbackLang string
	I18nDir      string // Absolute path to directory containing .json files
}
