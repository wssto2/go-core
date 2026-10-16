package go2ts_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/go2ts"
)

type syncUser struct {
	ID   int    `json:"id"`
	Name string `json:"name,omitzero"`
}

func TestSync_WritesOnlyChangedFilesAndDeletesStaleOnes(t *testing.T) {
	dir := t.TempDir()
	gen := func(d string) error { return go2ts.GenerateTypes([]any{syncUser{}}, d) }

	require.NoError(t, go2ts.Sync(dir, gen))

	file := filepath.Join(dir, "syncUser.ts")
	require.FileExists(t, file)

	// a long-gone mtime shows whether the second run rewrote the file
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, os.Chtimes(file, old, old))

	stale := filepath.Join(dir, "Gone.ts")
	require.NoError(t, os.WriteFile(stale, []byte("// old"), 0o600))

	keptOther := filepath.Join(dir, "notes.txt")
	require.NoError(t, os.WriteFile(keptOther, []byte("mine"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub"), 0o755))

	nested := filepath.Join(dir, "sub", "Nested.ts")
	require.NoError(t, os.WriteFile(nested, []byte("// nested"), 0o600))

	require.NoError(t, go2ts.Sync(dir, gen))

	info, err := os.Stat(file)
	require.NoError(t, err)
	assert.True(t, info.ModTime().Equal(old), "an unchanged file is not rewritten")
	assert.NoFileExists(t, stale, "a stale .ts is deleted")
	assert.FileExists(t, keptOther, "another extension is left alone")
	assert.FileExists(t, nested, "a subfolder is left alone")

	// a changed file is rewritten
	require.NoError(t, os.WriteFile(file, []byte("changed"), 0o600))
	require.NoError(t, go2ts.Sync(dir, gen))

	got, err := os.ReadFile(file) //nolint:gosec // a file of the test's temp dir
	require.NoError(t, err)
	assert.Contains(t, string(got), "export type syncUser")
}

func TestSync_AFailingGeneratorLeavesTheFolderAlone(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "Keep.ts")
	require.NoError(t, os.WriteFile(keep, []byte("// keep"), 0o600))

	boom := errors.New("boom")

	err := go2ts.Sync(dir, func(string) error { return boom })

	require.ErrorIs(t, err, boom)
	assert.FileExists(t, keep)
}

func TestOmitzeroIsOptionalLikeOmitempty(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, go2ts.GenerateTypes([]any{syncUser{}}, dir))

	got, err := os.ReadFile(filepath.Join(dir, "syncUser.ts")) //nolint:gosec // a file of the test's temp dir
	require.NoError(t, err)
	assert.Contains(t, string(got), "  name?: string;")
	assert.Contains(t, string(got), "  id: number;")
}

type syncInput struct {
	Name string `json:"name,omitzero" validation:"max:10"`
	Must string `json:"must,omitzero" validation:"required"`
}

func TestOmitzeroSchemaFieldIsOptionalUnlessRequired(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, go2ts.GenerateSchemas([]any{syncInput{}}, dir))

	got, err := os.ReadFile(filepath.Join(dir, "syncInput.ts")) //nolint:gosec // a file of the test's temp dir
	require.NoError(t, err)
	assert.Contains(t, string(got), "  name: z.string().max(10).optional(),")
	assert.Contains(t, string(got), "  must: z.string().min(1),")
}
