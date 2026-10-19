package apperr_test

import (
	"errors"
	"fmt"
	"os"
	"strings"
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

func TestGetHTTPStatus_WrappedAppError_Unwraps(t *testing.T) {
	t.Parallel()

	inner := apperr.NotFound("resource not found")
	wrapped := fmt.Errorf("service layer: %w", inner)

	status := apperr.GetHTTPStatus(wrapped)
	require.Equal(t, 404, status, "GetHTTPStatus must unwrap wrapped AppError")
}

func TestGetHTTPStatus_PlainError_Returns500(t *testing.T) {
	t.Parallel()

	status := apperr.GetHTTPStatus(errors.New("plain error")) //nolint:err113
	require.Equal(t, 500, status)
}

func TestEveryCodeHasAMessageAndAFromForm(t *testing.T) {
	t.Parallel()

	cause := errors.New("cause") //nolint:err113
	for code, pair := range map[apperr.Code][2]*apperr.AppError{
		apperr.CodeBadRequest:       {apperr.BadRequest("m"), apperr.BadRequestFrom(cause)},
		apperr.CodeNotFound:         {apperr.NotFound("m"), apperr.NotFoundFrom(cause)},
		apperr.CodePermissionDenied: {apperr.Forbidden("m"), apperr.ForbiddenFrom(cause)},
		apperr.CodeAlreadyExists:    {apperr.Conflict("m"), apperr.ConflictFrom(cause)},
	} {
		require.Equal(t, code, pair[0].Code)
		require.Equal(t, code, pair[1].Code)
		require.ErrorIs(t, pair[1], cause)
	}
}

var errTicketGone = errors.New("ticket gone")

func TestWhen(t *testing.T) {
	t.Parallel()

	t.Run("nil stays nil", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, apperr.When(nil, apperr.On(errTicketGone, apperr.NotFound("x"))))
	})

	t.Run("first match maps and keeps the cause", func(t *testing.T) {
		t.Parallel()

		other := errors.New("other") //nolint:err113
		cause := fmt.Errorf("find: %w", errTicketGone)
		got := apperr.When(cause,
			apperr.On(other, apperr.BadRequest("no")),
			apperr.On(errTicketGone, apperr.NotFound("ticket not found")),
			apperr.On(errTicketGone, apperr.Conflict("later case")),
		)

		var ae *apperr.AppError
		require.ErrorAs(t, got, &ae)
		require.Equal(t, apperr.CodeNotFound, ae.Code)
		require.Equal(t, "ticket not found", ae.Message)
		require.ErrorIs(t, got, errTicketGone)
	})

	t.Run("an AppError passes unchanged", func(t *testing.T) {
		t.Parallel()

		in := apperr.Forbidden("no")
		require.Same(t, in, apperr.When(in, apperr.On(errTicketGone, apperr.NotFound("x"))))

		wrapped := fmt.Errorf("ctx: %w", in)
		require.Equal(t, wrapped, apperr.When(wrapped))
	})

	t.Run("no match is Internal at the callers line", func(t *testing.T) {
		t.Parallel()

		boom := errors.New("boom")                                               //nolint:err113
		got := apperr.When(boom, apperr.On(errTicketGone, apperr.NotFound("x"))) // WHEN-CALL

		var ae *apperr.AppError
		require.ErrorAs(t, got, &ae)
		require.Equal(t, apperr.CodeInternal, ae.Code)
		require.ErrorIs(t, got, boom)
		require.True(t, strings.HasSuffix(ae.File, "apperr_test.go"), ae.File)
		require.Equal(t, lineOf(t, "// WHEN-CALL"), ae.Line)
	})
}

func lineOf(t *testing.T, marker string) int {
	t.Helper()

	src, err := os.ReadFile("apperr_test.go")
	require.NoError(t, err)

	for i, l := range strings.Split(string(src), "\n") {
		if strings.HasSuffix(l, marker) {
			return i + 1
		}
	}

	t.Fatalf("marker %s not found", marker)

	return 0
}
