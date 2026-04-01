package apperr_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/apperr"
)

func TestConstructorsAndErrorString(t *testing.T) {
	t.Parallel()

	br := apperr.BadRequest("invalid input")
	require.Equal(t, apperr.CodeBadRequest, br.Code)
	require.Equal(t, "invalid input", br.Message)
	require.Contains(t, br.Error(), "invalid input")

	nf := apperr.NotFound("missing")
	require.Equal(t, apperr.CodeNotFound, nf.Code)
	require.Contains(t, nf.Error(), "missing")

	orig := errors.New("boom") //nolint:err113
	intErr := apperr.Internal(orig)
	require.Equal(t, apperr.CodeInternal, intErr.Code)
	require.Contains(t, intErr.Error(), "boom")
}

func TestWrapPreserveKeepsCode(t *testing.T) {
	t.Parallel()

	base := apperr.BadRequest("bad field")
	wrapped := apperr.WrapPreserve(base, "while saving")

	require.Equal(t, base.Code, wrapped.Code)
	require.Error(t, wrapped.Err)
	require.ErrorAs(t, wrapped.Err, &base)
	require.Contains(t, wrapped.Message, "while saving")
	require.Contains(t, wrapped.Message, base.Message)
}

func TestWrapAddsContext(t *testing.T) {
	t.Parallel()

	err := errors.New("underlying") //nolint:err113
	w := apperr.Wrap(err, "higher level", apperr.CodeInternal)

	require.Equal(t, apperr.CodeInternal, w.Code)
	require.Error(t, w.Err)
	require.Equal(t, err, w.Err)
	require.Contains(t, w.Error(), "higher level")
}
