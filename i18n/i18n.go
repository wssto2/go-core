// go-core/i18n/i18n.go
package i18n

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/goccy/go-json"
)

type ctxKey struct{}

// Translator holds translations for a configured set of languages.
// Create one at startup and inject it into services that need it.
type Translator struct {
	mu           sync.RWMutex
	translations map[string]string
	cfg          Config
}

// New creates and initializes a Translator. Returns an error if loading fails.
func New(cfg Config) (*Translator, error) {
	if cfg.FallbackLang == "" {
		cfg.FallbackLang = "en"
	}
	t := &Translator{cfg: cfg}
	return t, t.Load()
}

// MustNew creates and initializes a Translator. Panics if loading fails.
func MustNew(cfg Config) *Translator {
	t, err := New(cfg)
	if err != nil {
		panic(err)
	}
	return t
}

// T translates a key into the target language.
func (t *Translator) T(key, lang string) string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.translations == nil {
		return key
	}
	if val, ok := t.translations[lang+"."+key]; ok {
		return val
	}
	if lang != t.cfg.FallbackLang {
		if val, ok := t.translations[t.cfg.FallbackLang+"."+key]; ok {
			return val
		}
	}
	return key
}

// TWith translates a key into the target language with parameters.
func (t *Translator) TWith(key, lang string, params map[string]any) string {
	msg := t.T(key, lang)
	for k, v := range params {
		msg = strings.ReplaceAll(msg, ":"+k, fmt.Sprintf("%v", v))
	}
	return msg
}

// Load (re)loads translations from disk. Safe to call at runtime for hot-reload.
func (t *Translator) Load() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.cfg.I18nDir == "" {
		return fmt.Errorf("i18n: I18nDir is not configured")
	}

	newTranslations := make(map[string]string)
	files, err := os.ReadDir(t.cfg.I18nDir)
	if err != nil {
		return fmt.Errorf("i18n: failed to read directory %s: %w", t.cfg.I18nDir, err)
	}

	for _, file := range files {
		if file.IsDir() || filepath.Ext(file.Name()) != ".json" {
			continue
		}

		path := filepath.Join(t.cfg.I18nDir, file.Name())
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("i18n: failed to read file %s: %w", path, err)
		}

		var data map[string]any
		if err := json.Unmarshal(content, &data); err != nil {
			return fmt.Errorf("i18n: failed to parse file %s: %w", path, err)
		}

		lang := strings.TrimSuffix(file.Name(), ".json")
		flattenJSON("", lang+".", data, newTranslations)
	}

	t.translations = newTranslations
	return nil
}

func GetFromContext(ctx context.Context) *Translator {
	if t := ctx.Value(ctxKey{}); t != nil {
		return t.(*Translator)
	}
	return nil
}
