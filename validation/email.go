package validation

import (
	"github.com/wssto2/go-core/i18n"
	"github.com/wssto2/go-core/utils"
)

func EmailRule(ctx ValidationContext, attribute string, value any, args string, required bool, fail func(string), subject any) {
	lang := ctx.Locale()
	if lang == "" {
		lang = "en"
	}

	valueStr, ok := value.(string)
	if !ok {
		if value == nil && !required {
			return
		}
		fail(i18n.T("validation_errors.email", lang))
		return
	}

	if valueStr == "" && !required {
		return
	}

	if !utils.IsValidEmail(valueStr) {
		fail(i18n.T("validation_errors.email", lang))
	}
}
