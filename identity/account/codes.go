package account

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"time"

	"github.com/wssto2/go-core/apperr"
)

// Purpose names what a code authorizes. It is bound into the code's hash, so a
// code issued for one purpose never satisfies another.
type Purpose string

const (
	// PurposeEmailChange confirms a new e-mail address; the code's Target is the address.
	PurposeEmailChange Purpose = "email_change"
	// PurposePasswordReset is reserved for a forgotten-password flow.
	PurposePasswordReset Purpose = "password_reset"
)

// CodeLength is the number of digits in a code (IAM-OTP-001).
const CodeLength = 6

// MinCodeSecret is the shortest secret the codes' HMAC accepts.
const MinCodeSecret = 32

// CodeRules are the limits of one-time codes (IAM-OTP-002, IAM-OTP-003). The
// zero value is the defaults.
type CodeRules struct {
	// TTL is how long a code stays usable from sending. Default 15 minutes.
	TTL time.Duration
	// Attempts is how many wrong guesses kill a code. Default 5.
	Attempts int
	// Cooldown is the least time between two sends for one purpose. Default 60 seconds.
	Cooldown time.Duration
	// PerHour is how many codes one account may be sent per purpose in a
	// rolling hour, so the resend button cannot flood a mailbox. Default 5.
	PerHour int
}

func (r CodeRules) withDefaults() CodeRules {
	if r.TTL <= 0 {
		r.TTL = 15 * time.Minute
	}

	if r.Attempts <= 0 {
		r.Attempts = 5
	}

	if r.Cooldown <= 0 {
		r.Cooldown = 60 * time.Second
	}

	if r.PerHour <= 0 {
		r.PerHour = 5
	}

	return r
}

const codeWindow = time.Hour

// Code is one issued one-time code. The plain code is never held: only Hash, an
// HMAC over the account, the purpose and the code.
type Code struct {
	ID            int
	AccountID     int
	Purpose       Purpose
	Target        string
	Hash          string
	Attempts      int
	ExpiresAt     time.Time
	ConsumedAt    *time.Time
	InvalidatedAt *time.Time
	IP            string
	CreatedAt     time.Time
}

// Live reports whether the code can still be verified at now: not used, not
// invalidated and not expired.
func (c Code) Live(now time.Time) bool {
	return c.ConsumedAt == nil && c.InvalidatedAt == nil && now.Before(c.ExpiresAt)
}

// Error values of the code stores.
var (
	// ErrCodeNotFound is what CodeStore.Latest returns when no code was ever
	// issued for the account and purpose.
	ErrCodeNotFound = errors.New("identity: no code was issued")
	// ErrCodeConflict is what CodeStore.SaveVerification returns when the code
	// was verified by another request since it was read; nothing was written.
	ErrCodeConflict = errors.New("identity: the code changed since it was read")
)

// CodeStore keeps one-time codes. Every method is keyed on account and purpose.
type CodeStore interface {
	// Latest returns the most recently issued code, live or not, or ErrCodeNotFound.
	Latest(ctx context.Context, accountID int, p Purpose) (Code, error)
	// IssuedSince counts the codes issued after since and returns the creation
	// time of the oldest of them (zero when there are none).
	IssuedSince(ctx context.Context, accountID int, p Purpose, since time.Time) (n int, oldest time.Time, err error)
	// Issue ends every live code of the account and purpose and stores c, in one
	// transaction (IAM-OTP-004), and returns it with its ID.
	Issue(ctx context.Context, c Code, now time.Time) (Code, error)
	// SaveVerification stores what a verification did to a code that was read with
	// readAttempts wrong guesses: Attempts, ConsumedAt and InvalidatedAt. It
	// writes only while the stored code is still unconsumed, not invalidated and
	// at readAttempts, so of two verifications of one read exactly one is saved,
	// and a code is consumed at most once; otherwise it writes nothing and
	// returns ErrCodeConflict.
	SaveVerification(ctx context.Context, c Code, readAttempts int, now time.Time) error
	// InvalidateLive ends every live code of the account and purpose.
	InvalidateLive(ctx context.Context, accountID int, p Purpose, now time.Time) error
}

// CodeMessage is everything a CodeSender needs to deliver one code.
type CodeMessage struct {
	Purpose Purpose
	// Recipient is the address to send to; Name is the person's name.
	Recipient string
	Name      string
	// Locale is the person's language, for the text of the message.
	Locale    string
	Code      string
	ExpiresAt time.Time
}

// CodeSender delivers a code to its recipient. A failed delivery is an error:
// the person cannot go on without the code.
type CodeSender interface {
	SendCode(ctx context.Context, m CodeMessage) error
}

// Reasons of the code failures (IAM-OTP-002, IAM-OTP-003).
const (
	// ReasonCodeResendTooSoon is a new code asked for within the cooldown; params.retry_after is seconds.
	ReasonCodeResendTooSoon apperr.Reason = "identity.code.resend_too_soon"
	// ReasonCodeTooManyRequests is the cap of codes per hour; params.retry_after is seconds.
	ReasonCodeTooManyRequests apperr.Reason = "identity.code.too_many_requests"
	// ReasonCodeNoPending is a code to verify when none was ever issued.
	ReasonCodeNoPending apperr.Reason = "identity.code.no_pending"
	// ReasonCodeExpired is a code that is expired, used, cancelled or out of attempts.
	ReasonCodeExpired apperr.Reason = "identity.code.expired"
	// ReasonCodeMismatch is a wrong code; params.attempts_left says how many guesses remain.
	ReasonCodeMismatch apperr.Reason = "identity.code.mismatch"
	// ReasonCodeTooManyAttempts is the wrong guess that used the last attempt.
	ReasonCodeTooManyAttempts apperr.Reason = "identity.code.too_many_attempts"
	// ReasonCodeNotDelivered is a code that could not be sent; it was invalidated.
	ReasonCodeNotDelivered apperr.Reason = "identity.code.not_delivered"
)

// CodesDeps are what Codes is built on; all are required.
type CodesDeps struct {
	Store  CodeStore
	Sender CodeSender
	Clock  Clock
	// Secret keys the HMAC of the stored hashes, at least MinCodeSecret
	// characters. It is server-side only, so a leaked table cannot be searched
	// offline across the million possible codes.
	Secret string
}

// Codes issues and verifies one-time codes for any purpose (IAM-OTP-001..004).
// It knows nothing about what a code authorizes: the caller picks the purpose and
// the target and decides what happens once a code is verified.
type Codes struct {
	deps  CodesDeps
	rules CodeRules
}

// NewCodes builds the service, or says which dependency is missing.
func NewCodes(d CodesDeps, rules CodeRules) (*Codes, error) {
	switch {
	case d.Store == nil:
		return nil, errors.New("identity: CodesDeps.Store is missing: pass a CodeStore, for example gormstore.New(db).Codes")
	case d.Sender == nil:
		return nil, errors.New("identity: CodesDeps.Sender is missing: pass a CodeSender (identity builds one from a mail.Sender)")
	case d.Clock == nil:
		return nil, errors.New("identity: CodesDeps.Clock is missing: pass the application's clock, app.Clock()")
	case len(d.Secret) < MinCodeSecret:
		return nil, fmt.Errorf("identity: CodesDeps.Secret needs at least %d characters: set a long random secret, for example from an environment variable", MinCodeSecret)
	}

	return &Codes{deps: d, rules: rules.withDefaults()}, nil
}

// IssueCode is one code to send. Recipient is the address it goes to, which for
// an e-mail change is the new address, the Target the code confirms.
type IssueCode struct {
	AccountID int
	Purpose   Purpose
	// Target is what the code confirms (the new address); empty when the purpose needs none.
	Target    string
	Recipient string
	// Name and Locale are the person's, for the message.
	Name   string
	Locale string
	// IP is the caller's address, kept for the record.
	IP string
}

// PendingCode is the visible state of a live code. It never carries the code.
type PendingCode struct {
	Target            string
	ExpiresAt         time.Time
	ResendAvailableAt time.Time
	AttemptsLeft      int
}

// Issue sends a new code and ends any live one of the account and purpose
// (IAM-OTP-001, IAM-OTP-003, IAM-OTP-004), subject to the cooldown and the cap.
// When delivery fails the new code is ended again (IAM-OTP-003.4), so a code
// nobody received is never live.
func (c *Codes) Issue(ctx context.Context, in IssueCode) (PendingCode, error) {
	now := c.deps.Clock.Now()

	if err := c.checkCanIssue(ctx, in, now); err != nil {
		return PendingCode{}, err
	}

	plain, err := newCode()
	if err != nil {
		return PendingCode{}, apperr.Internal(err)
	}

	code, err := c.deps.Store.Issue(ctx, Code{
		AccountID: in.AccountID, Purpose: in.Purpose, Target: in.Target, Hash: c.hash(in.AccountID, in.Purpose, plain),
		ExpiresAt: now.Add(c.rules.TTL), IP: cut(in.IP, IPMax), CreatedAt: now,
	}, now)
	if err != nil {
		return PendingCode{}, apperr.Internal(err)
	}

	sendErr := c.deps.Sender.SendCode(ctx, CodeMessage{
		Purpose: in.Purpose, Recipient: in.Recipient, Name: in.Name, Locale: in.Locale, Code: plain, ExpiresAt: code.ExpiresAt,
	})
	if sendErr != nil {
		if err := c.deps.Store.InvalidateLive(ctx, in.AccountID, in.Purpose, c.deps.Clock.Now()); err != nil {
			return PendingCode{}, apperr.Internal(errors.Join(sendErr, err))
		}

		return PendingCode{}, apperr.Wrap(sendErr, string(ReasonCodeNotDelivered), apperr.CodeInternal).WithReason(ReasonCodeNotDelivered)
	}

	return c.pending(code), nil
}

func (c *Codes) checkCanIssue(ctx context.Context, in IssueCode, now time.Time) error {
	latest, err := c.deps.Store.Latest(ctx, in.AccountID, in.Purpose)
	if err != nil && !errors.Is(err, ErrCodeNotFound) {
		return apperr.Internal(err)
	}

	if err == nil && now.Before(latest.CreatedAt.Add(c.rules.Cooldown)) {
		return retryAfter(ReasonCodeResendTooSoon, latest.CreatedAt.Add(c.rules.Cooldown).Sub(now), apperr.LevelInfo)
	}

	n, oldest, err := c.deps.Store.IssuedSince(ctx, in.AccountID, in.Purpose, now.Add(-codeWindow))
	if err != nil {
		return apperr.Internal(err)
	}

	if n >= c.rules.PerHour {
		return retryAfter(ReasonCodeTooManyRequests, oldest.Add(codeWindow).Sub(now), apperr.LevelWarn)
	}

	return nil
}

func retryAfter(reason apperr.Reason, d time.Duration, level apperr.Level) error {
	return apperr.New(nil, string(reason), apperr.CodeBadRequest).
		WithReason(reason, map[string]any{"retry_after": max(int(math.Ceil(d.Seconds())), 1)}).WithLog(level)
}

// Verify checks the code against the latest one issued and returns the target it
// confirms (IAM-OTP-002). A match consumes the code; a wrong guess costs an
// attempt, the last one ends the code; a code that is no longer live asks for a
// new one. The outcome is saved only over the state the code was read in, and a
// request that lost the race reads again, so parallel wrong guesses each cost
// an attempt and a code is consumed at most once.
func (c *Codes) Verify(ctx context.Context, accountID int, p Purpose, plain string) (string, error) {
	hash := c.hash(accountID, p, plain)

	// Every conflict means another verification was saved, and a code survives at
	// most Attempts of those, so the rounds end on a definite answer.
	for range c.rules.Attempts + 1 {
		target, err := c.verifyOnce(ctx, accountID, p, hash)
		if !errors.Is(err, ErrCodeConflict) {
			return target, err
		}
	}

	return "", apperr.Internal(ErrCodeConflict)
}

func (c *Codes) verifyOnce(ctx context.Context, accountID int, p Purpose, hash string) (string, error) {
	code, err := c.deps.Store.Latest(ctx, accountID, p)
	if errors.Is(err, ErrCodeNotFound) {
		return "", apperr.New(nil, string(ReasonCodeNoPending), apperr.CodeInvalidArgument).WithReason(ReasonCodeNoPending).WithLog(apperr.LevelInfo)
	}

	if err != nil {
		return "", apperr.Internal(err)
	}

	now := c.deps.Clock.Now()
	if !code.Live(now) {
		return "", apperr.New(nil, string(ReasonCodeExpired), apperr.CodeInvalidArgument).WithReason(ReasonCodeExpired).WithLog(apperr.LevelInfo)
	}

	read := code.Attempts
	matched := subtle.ConstantTimeCompare([]byte(code.Hash), []byte(hash)) == 1

	var refusal error

	switch {
	case matched:
		code.ConsumedAt = &now
	default:
		code.Attempts++
		if code.Attempts >= c.rules.Attempts {
			code.InvalidatedAt = &now
			refusal = apperr.New(nil, string(ReasonCodeTooManyAttempts), apperr.CodeInvalidArgument).WithReason(ReasonCodeTooManyAttempts).WithLog(apperr.LevelWarn)
		} else {
			refusal = apperr.New(nil, string(ReasonCodeMismatch), apperr.CodeInvalidArgument).
				WithReason(ReasonCodeMismatch, map[string]any{"attempts_left": c.rules.Attempts - code.Attempts}).WithLog(apperr.LevelInfo)
		}
	}

	if err := c.deps.Store.SaveVerification(ctx, code, read, now); err != nil {
		if errors.Is(err, ErrCodeConflict) {
			return "", err
		}

		return "", apperr.Internal(err)
	}

	if refusal != nil {
		return "", refusal
	}

	return code.Target, nil
}

// Cancel ends the live code of the account and purpose; having none is fine.
func (c *Codes) Cancel(ctx context.Context, accountID int, p Purpose) error {
	if err := c.deps.Store.InvalidateLive(ctx, accountID, p, c.deps.Clock.Now()); err != nil {
		return apperr.Internal(err)
	}

	return nil
}

// Pending returns the state of the latest code when it is still live.
func (c *Codes) Pending(ctx context.Context, accountID int, p Purpose) (PendingCode, bool, error) {
	code, err := c.deps.Store.Latest(ctx, accountID, p)
	if errors.Is(err, ErrCodeNotFound) {
		return PendingCode{}, false, nil
	}

	if err != nil {
		return PendingCode{}, false, apperr.Internal(err)
	}

	if !code.Live(c.deps.Clock.Now()) {
		return PendingCode{}, false, nil
	}

	return c.pending(code), true, nil
}

func (c *Codes) pending(code Code) PendingCode {
	return PendingCode{
		Target: code.Target, ExpiresAt: code.ExpiresAt, ResendAvailableAt: code.CreatedAt.Add(c.rules.Cooldown),
		AttemptsLeft: max(c.rules.Attempts-code.Attempts, 0),
	}
}

// hash binds the account and the purpose into what is stored.
func (c *Codes) hash(accountID int, p Purpose, plain string) string {
	mac := hmac.New(sha256.New, []byte(c.deps.Secret))
	mac.Write([]byte(strconv.Itoa(accountID) + "|" + string(p) + "|" + plain))

	return hex.EncodeToString(mac.Sum(nil))
}

// newCode draws CodeLength digits uniformly from crypto/rand.
func newCode() (string, error) {
	n, err := rand.Int(rand.Reader, new(big.Int).Exp(big.NewInt(10), big.NewInt(CodeLength), nil))
	if err != nil {
		return "", fmt.Errorf("identity: reading random digits: %w", err)
	}

	return fmt.Sprintf("%0*d", CodeLength, n.Int64()), nil
}
