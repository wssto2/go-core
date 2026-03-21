// go-core/i18n/i18n.go
package i18n

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/goccy/go-json"
)

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

		var data map[string]interface{}
		if err := json.Unmarshal(content, &data); err != nil {
			return fmt.Errorf("i18n: failed to parse file %s: %w", path, err)
		}

		lang := strings.TrimSuffix(file.Name(), ".json")
		flattenJSON("", lang+".", data, newTranslations)
	}

	t.translations = newTranslations
	return nil
}

// --- Package-level convenience (backward compatibility) ---
// If you want to keep the old `i18n.T(key, lang)` API during migration,
// expose a default instance. But this is optional — prefer injection.

var Default *Translator

// Init initializes the package-level default Translator.
// Kept for backward compatibility. Prefer i18n.New() + injection.
func Init(cfg Config) error {
	t, err := New(cfg)
	if err != nil {
		return err
	}
	Default = t
	return nil
}

// T translates using the package-level default Translator.
// Panics if Init has not been called.
func T(key, lang string) string {
	if Default == nil {
		return key // graceful degradation instead of panic
	}
	return Default.T(key, lang)
}
