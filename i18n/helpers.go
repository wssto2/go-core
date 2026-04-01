package i18n

import "github.com/wssto2/go-core/utils"

func flattenJSON(prefix, lang string, data any, result map[string]string) {
	switch typedValue := data.(type) {
	case map[string]any:
		for k, val := range typedValue {
			newPrefix := k
			if prefix != "" {
				newPrefix = prefix + "." + k
			}
			flattenJSON(newPrefix, lang, val, result)
		}
	default:
		if prefix != "" {
			result[lang+prefix] = utils.ToString(typedValue)
		}
	}
}
