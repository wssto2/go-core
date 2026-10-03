package account_test

import (
	"context"
	"errors"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/identity/account"
	"github.com/wssto2/go-core/identity/identitytest"
)

func issue(t *testing.T, k identitytest.Kit) account.PendingCode {
	t.Helper()

	p, err := k.Codes.Issue(t.Context(), account.IssueCode{
		AccountID: 1, Purpose: account.PurposeEmailChange, Target: "new@example.test", Recipient: "new@example.test", Name: "Ana", Locale: "hr", IP: "10.0.0.1",
	})
	require.NoError(t, err)

	return p
}

func wrong(code string) string {
	if code == "000000" {
		return "000001"
	}

	return "000000"
}

// IAM-OTP-001: six digits, mailed to the recipient with the account's language, never stored plain.
func TestIssueSendsASixDigitCode(t *testing.T) {
	k := seeded(t)
	p := issue(t, k)

	msg := k.Mailbox.Last()
	require.Regexp(t, regexp.MustCompile(`^\d{6}$`), msg.Code)
	require.Equal(t, "new@example.test", msg.Recipient)
	require.Equal(t, "hr", msg.Locale)
	require.Equal(t, account.PurposeEmailChange, msg.Purpose)
	require.Equal(t, "new@example.test", p.Target)
	require.Equal(t, 5, p.AttemptsLeft)
	require.Equal(t, identitytest.Epoch.Add(15*time.Minute), p.ExpiresAt)
	require.Equal(t, identitytest.Epoch.Add(time.Minute), p.ResendAvailableAt)

	stored, err := identitytest.NewCodes().Latest(t.Context(), 1, account.PurposeEmailChange)
	require.ErrorIs(t, err, account.ErrCodeNotFound)
	require.Empty(t, stored.Hash)
}

// IAM-OTP-001: the target comes back with the code that confirms it, and a code of one
// purpose or account never satisfies another.
func TestVerifyReturnsTheTargetAndBindsPurposeAndAccount(t *testing.T) {
	k := seeded(t)
	issue(t, k)
	code := k.Mailbox.Last().Code

	_, err := k.Codes.Verify(t.Context(), 1, account.PurposePasswordReset, code)
	require.True(t, apperr.HasReason(err, account.ReasonCodeNoPending), "another purpose has no code")

	_, err = k.Codes.Verify(t.Context(), 2, account.PurposeEmailChange, code)
	require.True(t, apperr.HasReason(err, account.ReasonCodeNoPending), "another account has no code")

	target, err := k.Codes.Verify(t.Context(), 1, account.PurposeEmailChange, code)
	require.NoError(t, err)
	require.Equal(t, "new@example.test", target)
}

// IAM-OTP-002: used once.
func TestACodeIsUsedOnce(t *testing.T) {
	k := seeded(t)
	issue(t, k)
	code := k.Mailbox.Last().Code

	_, err := k.Codes.Verify(t.Context(), 1, account.PurposeEmailChange, code)
	require.NoError(t, err)

	_, err = k.Codes.Verify(t.Context(), 1, account.PurposeEmailChange, code)
	require.True(t, apperr.HasReason(err, account.ReasonCodeExpired))

	_, live, err := k.Codes.Pending(t.Context(), 1, account.PurposeEmailChange)
	require.NoError(t, err)
	require.False(t, live)
}

// IAM-OTP-002: 15 minutes.
func TestACodeExpires(t *testing.T) {
	k := seeded(t)
	issue(t, k)
	code := k.Mailbox.Last().Code

	k.Clock.Advance(15*time.Minute - time.Second)
	_, live, _ := k.Codes.Pending(t.Context(), 1, account.PurposeEmailChange)
	require.True(t, live)

	k.Clock.Advance(time.Second)
	_, err := k.Codes.Verify(t.Context(), 1, account.PurposeEmailChange, code)
	require.True(t, apperr.HasReason(err, account.ReasonCodeExpired))
}

// IAM-OTP-002: a wrong guess costs an attempt, the 5th ends the code, even for the right one after.
func TestWrongGuessesCostAttemptsAndTheFifthEndsTheCode(t *testing.T) {
	k := seeded(t)
	issue(t, k)
	code := k.Mailbox.Last().Code

	for left := 4; left >= 1; left-- {
		_, err := k.Codes.Verify(t.Context(), 1, account.PurposeEmailChange, wrong(code))
		require.True(t, apperr.HasReason(err, account.ReasonCodeMismatch))

		var ae *apperr.AppError
		require.ErrorAs(t, err, &ae)
		require.Equal(t, left, ae.Params["attempts_left"])
	}

	_, err := k.Codes.Verify(t.Context(), 1, account.PurposeEmailChange, wrong(code))
	require.True(t, apperr.HasReason(err, account.ReasonCodeTooManyAttempts))

	_, err = k.Codes.Verify(t.Context(), 1, account.PurposeEmailChange, code)
	require.True(t, apperr.HasReason(err, account.ReasonCodeExpired), "the right code no longer works")
}

// IAM-OTP-003: 60 seconds between sends, params say how long.
func TestResendCooldown(t *testing.T) {
	k := seeded(t)
	issue(t, k)

	_, err := k.Codes.Issue(t.Context(), account.IssueCode{AccountID: 1, Purpose: account.PurposeEmailChange, Target: "x@example.test", Recipient: "x@example.test"})
	require.True(t, apperr.HasReason(err, account.ReasonCodeResendTooSoon))

	var ae *apperr.AppError
	require.ErrorAs(t, err, &ae)
	require.Equal(t, 60, ae.Params["retry_after"])
	require.Len(t, k.Mailbox.Sent(), 1, "nothing was sent")

	k.Clock.Advance(59 * time.Second)
	_, err = k.Codes.Issue(t.Context(), account.IssueCode{AccountID: 1, Purpose: account.PurposeEmailChange, Recipient: "x@example.test"})
	require.ErrorAs(t, err, &ae)
	require.Equal(t, 1, ae.Params["retry_after"])

	k.Clock.Advance(time.Second)
	_, err = k.Codes.Issue(t.Context(), account.IssueCode{AccountID: 1, Purpose: account.PurposeEmailChange, Recipient: "x@example.test"})
	require.NoError(t, err)
}

// IAM-OTP-003: five an hour per purpose; the other purpose and a later hour are free.
func TestHourlyCap(t *testing.T) {
	k := seeded(t)

	for range 5 {
		issue(t, k)
		k.Clock.Advance(time.Minute)
	}

	_, err := k.Codes.Issue(t.Context(), account.IssueCode{AccountID: 1, Purpose: account.PurposeEmailChange, Recipient: "x@example.test"})
	require.True(t, apperr.HasReason(err, account.ReasonCodeTooManyRequests))

	var ae *apperr.AppError
	require.ErrorAs(t, err, &ae)
	require.Equal(t, 55*60, ae.Params["retry_after"], "when the oldest of the five leaves the hour")

	_, err = k.Codes.Issue(t.Context(), account.IssueCode{AccountID: 1, Purpose: account.PurposePasswordReset, Recipient: "x@example.test"})
	require.NoError(t, err, "the cap is per purpose")

	k.Clock.Advance(55 * time.Minute)
	issue(t, k)
}

// IAM-OTP-003: a fresh code each time; IAM-OTP-004: one live code per account and purpose.
func TestAResendReplacesTheLiveCode(t *testing.T) {
	k := seeded(t)
	issue(t, k)
	first := k.Mailbox.Last().Code

	k.Clock.Advance(time.Minute)
	issue(t, k)
	second := k.Mailbox.Last().Code

	if first != second { // one in a million are alike
		_, err := k.Codes.Verify(t.Context(), 1, account.PurposeEmailChange, first)
		require.True(t, apperr.HasReason(err, account.ReasonCodeMismatch), "the old code is not the live one")
	}

	_, err := k.Codes.Verify(t.Context(), 1, account.PurposeEmailChange, second)
	require.NoError(t, err)
}

// IAM-OTP-003: a code nobody received is never live, and it still counts toward the cooldown.
func TestACodeThatCannotBeSentIsEnded(t *testing.T) {
	k := seeded(t)
	k.Mailbox.Fail = errors.New("relay down")

	_, err := k.Codes.Issue(t.Context(), account.IssueCode{AccountID: 1, Purpose: account.PurposeEmailChange, Recipient: "x@example.test"})
	require.True(t, apperr.HasReason(err, account.ReasonCodeNotDelivered))

	_, live, err := k.Codes.Pending(t.Context(), 1, account.PurposeEmailChange)
	require.NoError(t, err)
	require.False(t, live)

	k.Mailbox.Fail = nil
	_, err = k.Codes.Issue(t.Context(), account.IssueCode{AccountID: 1, Purpose: account.PurposeEmailChange, Recipient: "x@example.test"})
	require.True(t, apperr.HasReason(err, account.ReasonCodeResendTooSoon), "the failed send counts toward the cooldown")
}

func TestCancelEndsTheLiveCode(t *testing.T) {
	k := seeded(t)
	issue(t, k)

	require.NoError(t, k.Codes.Cancel(t.Context(), 1, account.PurposeEmailChange))
	require.NoError(t, k.Codes.Cancel(t.Context(), 1, account.PurposeEmailChange), "having none is fine")

	_, err := k.Codes.Verify(t.Context(), 1, account.PurposeEmailChange, k.Mailbox.Last().Code)
	require.True(t, apperr.HasReason(err, account.ReasonCodeExpired))
}

func TestRulesCanBeTuned(t *testing.T) {
	k := seeded(t, identitytest.WithCodeRules(account.CodeRules{TTL: time.Minute, Attempts: 2, Cooldown: time.Second, PerHour: 2}))
	issue(t, k)
	code := k.Mailbox.Last().Code

	_, err := k.Codes.Verify(t.Context(), 1, account.PurposeEmailChange, wrong(code))
	require.True(t, apperr.HasReason(err, account.ReasonCodeMismatch))
	_, err = k.Codes.Verify(t.Context(), 1, account.PurposeEmailChange, wrong(code))
	require.True(t, apperr.HasReason(err, account.ReasonCodeTooManyAttempts), "two attempts")

	k.Clock.Advance(time.Second)
	issue(t, k)
	k.Clock.Advance(time.Second)
	_, err = k.Codes.Issue(t.Context(), account.IssueCode{AccountID: 1, Purpose: account.PurposeEmailChange, Recipient: "x@example.test"})
	require.True(t, apperr.HasReason(err, account.ReasonCodeTooManyRequests), "two an hour")

	k.Clock.Advance(time.Minute)
	_, live, _ := k.Codes.Pending(t.Context(), 1, account.PurposeEmailChange)
	require.False(t, live, "one minute to live")
}

// IAM-OTP-002 item 5: parallel guesses each cost an attempt, and a code is consumed once.
func TestParallelGuessesEachCostAnAttempt(t *testing.T) {
	k := seeded(t)
	issue(t, k)
	code := k.Mailbox.Last().Code

	const workers = 12

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		reasons = map[apperr.Reason]int{}
	)

	for range workers {
		wg.Go(func() {
			_, err := k.Codes.Verify(context.Background(), 1, account.PurposeEmailChange, wrong(code))

			var ae *apperr.AppError

			mu.Lock()
			defer mu.Unlock()

			if errors.As(err, &ae) {
				reasons[ae.Reason]++
			} else {
				reasons["other"]++
			}
		})
	}

	wg.Wait()

	require.Equal(t, 4, reasons[account.ReasonCodeMismatch], "four wrong guesses are counted")
	require.Equal(t, 1, reasons[account.ReasonCodeTooManyAttempts], "the fifth ends the code")
	require.Equal(t, workers-5, reasons[account.ReasonCodeExpired], "the rest find it ended")
	require.Zero(t, reasons["other"])
}

func TestParallelRightGuessesConsumeTheCodeOnce(t *testing.T) {
	k := seeded(t)
	issue(t, k)
	code := k.Mailbox.Last().Code

	const workers = 12

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		wins int
	)

	for range workers {
		wg.Go(func() {
			if _, err := k.Codes.Verify(context.Background(), 1, account.PurposeEmailChange, code); err == nil {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		})
	}

	wg.Wait()
	require.Equal(t, 1, wins)
}

func TestNewCodesNamesWhatIsMissing(t *testing.T) {
	clock := identitytest.NewClock(identitytest.Epoch)

	for want, d := range map[string]account.CodesDeps{
		"CodesDeps.Store":  {Sender: &identitytest.Mailbox{}, Clock: clock, Secret: identitytest.CodeSecret},
		"CodesDeps.Sender": {Store: identitytest.NewCodes(), Clock: clock, Secret: identitytest.CodeSecret},
		"CodesDeps.Clock":  {Store: identitytest.NewCodes(), Sender: &identitytest.Mailbox{}, Secret: identitytest.CodeSecret},
		"at least 32":      {Store: identitytest.NewCodes(), Sender: &identitytest.Mailbox{}, Clock: clock, Secret: "short"},
	} {
		_, err := account.NewCodes(d, account.CodeRules{})
		require.ErrorContains(t, err, want)
	}
}
