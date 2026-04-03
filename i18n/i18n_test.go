package i18n_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/i18n"
)

// writeLangFile writes a JSON translation file into dir for the given lang.
func writeLangFile(t *testing.T, dir, lang, content string) {
	t.Helper()
	path := filepath.Join(dir, lang+".json")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func TestNew_LoadsTranslations(t *testing.T) {
	dir := t.TempDir()
	writeLangFile(t, dir, "en", `{"hello": "Hello", "nested": {"key": "Nested"}}`)
	writeLangFile(t, dir, "hr", `{"hello": "Bok"}`)

	tr, err := i18n.New(i18n.Config{I18nDir: dir})
	require.NoError(t, err)

	assert.Equal(t, "Hello", tr.T("hello", "en"))
	assert.Equal(t, "Nested", tr.T("nested.key", "en"))
	assert.Equal(t, "Bok", tr.T("hello", "hr"))
}

func TestT_FallsBackToFallbackLang(t *testing.T) {
	dir := t.TempDir()
	writeLangFile(t, dir, "en", `{"missing_in_hr": "English fallback"}`)
	writeLangFile(t, dir, "hr", `{}`)

	tr, err := i18n.New(i18n.Config{I18nDir: dir, FallbackLang: "en"})
	require.NoError(t, err)

	assert.Equal(t, "English fallback", tr.T("missing_in_hr", "hr"))
}

func TestT_ReturnKeyWhenMissing(t *testing.T) {
	dir := t.TempDir()
	writeLangFile(t, dir, "en", `{}`)

	tr, err := i18n.New(i18n.Config{I18nDir: dir})
	require.NoError(t, err)

	assert.Equal(t, "nonexistent.key", tr.T("nonexistent.key", "en"))
}

func TestT_DefaultFallbackLangIsEn(t *testing.T) {
	dir := t.TempDir()
	writeLangFile(t, dir, "en", `{"greet": "Hi"}`)

	// FallbackLang not set — should default to "en"
	tr, err := i18n.New(i18n.Config{I18nDir: dir})
	require.NoError(t, err)

	// Unknown lang falls back to en
	assert.Equal(t, "Hi", tr.T("greet", "fr"))
}

func TestTWith_InterpolatesParams(t *testing.T) {
	dir := t.TempDir()
	writeLangFile(t, dir, "en", `{"welcome": "Welcome, :name!"}`)

	tr, err := i18n.New(i18n.Config{I18nDir: dir})
	require.NoError(t, err)

	result := tr.TWith("welcome", "en", map[string]any{"name": "Alice"})
	assert.Equal(t, "Welcome, Alice!", result)
}

func TestTWith_MultipleParams(t *testing.T) {
	dir := t.TempDir()
	writeLangFile(t, dir, "en", `{"msg": ":a and :b"}`)

	tr, err := i18n.New(i18n.Config{I18nDir: dir})
	require.NoError(t, err)

	result := tr.TWith("msg", "en", map[string]any{"a": "foo", "b": "bar"})
	assert.Equal(t, "foo and bar", result)
}

func TestNew_ErrorOnMissingDir(t *testing.T) {
	_, err := i18n.New(i18n.Config{I18nDir: "/nonexistent/path/xyz"})
	assert.Error(t, err)
}

func TestNew_ErrorOnEmptyDir(t *testing.T) {
	_, err := i18n.New(i18n.Config{I18nDir: ""})
	assert.Error(t, err)
}

func TestNew_IgnoresNonJSONFiles(t *testing.T) {
	dir := t.TempDir()
	writeLangFile(t, dir, "en", `{"key": "value"}`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("ignore me"), 0o644))

	tr, err := i18n.New(i18n.Config{I18nDir: dir})
	require.NoError(t, err)
	assert.Equal(t, "value", tr.T("key", "en"))
}

func TestNew_ErrorOnInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "en.json"), []byte(`{invalid json`), 0o644))

	_, err := i18n.New(i18n.Config{I18nDir: dir})
	assert.Error(t, err)
}

func TestLoad_HotReload(t *testing.T) {
	dir := t.TempDir()
	writeLangFile(t, dir, "en", `{"key": "original"}`)

	tr, err := i18n.New(i18n.Config{I18nDir: dir})
	require.NoError(t, err)
	assert.Equal(t, "original", tr.T("key", "en"))

	// Overwrite the file and reload
	writeLangFile(t, dir, "en", `{"key": "updated"}`)
	require.NoError(t, tr.Load())

	assert.Equal(t, "updated", tr.T("key", "en"))
}

func TestMustNew_PanicsOnError(t *testing.T) {
	assert.Panics(t, func() {
		i18n.MustNew(i18n.Config{I18nDir: "/nonexistent"})
	})
}
