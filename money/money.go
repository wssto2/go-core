package money

import (
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strconv"
	"strings"
)

const (
	centsPerUnit = 100
	decimalBase  = 10
)

// mustInt64 is v as an int64; a value outside the range is an impossible amount (about 9.2e16 of the
// major unit) and panics with an error wrapping ErrOverflow rather than wrap silently.
func mustInt64(v *big.Int) int64 {
	if !v.IsInt64() {
		panic(fmt.Errorf("%w: %s does not fit in int64 cents", ErrOverflow, v))
	}

	return v.Int64()
}

// Errors returned by the package.
var (
	ErrCurrency = errors.New("money: currency mismatch")
	ErrOverflow = errors.New("money: overflow")
	ErrDecimal  = errors.New("money: not a decimal number")
)

// Money is an amount in cents of one currency. The zero value is 0 with no currency, which combines
// with any currency. It is comparable. Add and Sub return ErrOverflow past int64; every other
// operation (rounding, FromDecimal, FromFloat, Allocate, PercentOf) panics with an error wrapping
// ErrOverflow when the result does not fit int64 cents (about 9.2e16 of the major unit, like an
// impossible amount).
type Money struct {
	cents    int64
	currency string
}

// New is an amount of cents in a currency ("EUR").
func New(cents int64, currency string) Money { return Money{cents: cents, currency: currency} }

// FromDecimal parses an exact decimal string such as "32726.50" or "-0.5". More than 2 decimals round
// half away from zero. Anything else (empty, exponent, spaces, "1,5") is ErrDecimal.
func FromDecimal(s, currency string) (Money, error) {
	val, err := parseDecimal(s)
	if err != nil {
		return Money{}, err
	}

	return Money{cents: roundScaled(val, big.NewInt(centsPerUnit)), currency: currency}, nil
}

// FromFloat reads a float64 that came from the database (a decimal(p,2) column). It goes through the
// shortest decimal text of f, so every value such a column holds comes back exact; it is NOT a way to
// turn a calculated float into money. NaN and infinities give the zero Money.
func FromFloat(f float64, currency string) Money {
	val, err := parseDecimal(strconv.FormatFloat(f, 'f', -1, 64))
	if err != nil {
		return Money{cents: 0, currency: currency}
	}

	return Money{cents: roundScaled(val, big.NewInt(centsPerUnit)), currency: currency}
}

// Cents is the amount in cents.
func (m Money) Cents() int64 { return m.cents }

// Currency is the currency code, "" when none.
func (m Money) Currency() string { return m.currency }

// Float is the amount as a float64, for writing back to the database or JSON only. Never calculate
// with it.
func (m Money) Float() float64 { return float64(m.cents) / centsPerUnit }

// String is the amount with two decimals and the currency: "32726.50 EUR".
func (m Money) String() string {
	abs, sign := uint64(m.cents), "" //nolint:gosec // negated below when negative
	if m.cents < 0 {
		abs, sign = -abs, "-"
	}

	out := fmt.Sprintf("%s%d.%02d", sign, abs/centsPerUnit, abs%centsPerUnit)
	if m.currency != "" {
		out += " " + m.currency
	}

	return out
}

// IsZero reports an amount of 0.
func (m Money) IsZero() bool { return m.cents == 0 }

// Neg is -m.
func (m Money) Neg() Money { return Money{cents: -m.cents, currency: m.currency} }

// Abs is |m|.
func (m Money) Abs() Money {
	if m.cents < 0 {
		return m.Neg()
	}

	return m
}

// Cmp compares the amounts (-1, 0, 1). It ignores the currency: comparing EUR with BAM compares
// the numbers only.
func (m Money) Cmp(other Money) int {
	switch {
	case m.cents < other.cents:
		return -1
	case m.cents > other.cents:
		return 1
	default:
		return 0
	}
}

func combine(a, b Money) (string, error) {
	switch {
	case a.currency == "" || a.currency == b.currency:
		return b.currency, nil
	case b.currency == "":
		return a.currency, nil
	default:
		return "", ErrCurrency
	}
}

// Add is m + other. Different currencies are ErrCurrency (an empty one combines with any); a sum beyond
// int64 is ErrOverflow.
func (m Money) Add(other Money) (Money, error) {
	cur, err := combine(m, other)
	if err != nil {
		return Money{}, err
	}

	sum := m.cents + other.cents
	if (m.cents^sum)&(other.cents^sum) < 0 {
		return Money{}, ErrOverflow
	}

	return Money{cents: sum, currency: cur}, nil
}

// Sub is m - other, with the errors of Add.
func (m Money) Sub(other Money) (Money, error) { return m.Add(other.Neg()) }

// Sum adds the amounts; no amounts is the zero Money.
func Sum(ms ...Money) (Money, error) {
	var total Money

	for _, one := range ms {
		var err error

		total, err = total.Add(one)
		if err != nil {
			return Money{}, err
		}
	}

	return total, nil
}

// MulRate is m x r / 100, rounded once.
func (m Money) MulRate(r Rate) Money { return FromMoney(m).MulRate(r).Round() }

// AddRate is m x (1 + r/100), rounded once: a gross amount from a net one.
func (m Money) AddRate(r Rate) Money { return FromMoney(m).AddRate(r).Round() }

// RemoveRate is m / (1 + r/100), rounded once: the net amount of a VAT-inclusive one. A rate of -100%
// panics (division by zero).
func (m Money) RemoveRate(r Rate) Money { return FromMoney(m).RemoveRate(r).Round() }

// MulRatio is m x num / den, rounded once. A zero den panics, like Go's integer division.
func (m Money) MulRatio(num, den int64) Money { return FromMoney(m).MulRatio(num, den).Round() }

// DivInt is m / n, rounded once. A zero n panics, like Go's integer division.
func (m Money) DivInt(n int64) Money { return m.MulRatio(1, n) }

// RoundMode is how RoundTo rounds to a step.
type RoundMode int

// The rounding modes of RoundTo. They mean math.Round, math.Ceil and math.Floor of amount/step, times step.
const (
	RoundNearest RoundMode = iota // to the nearest multiple, a tie away from zero: -250 to 500 is -500
	RoundUp                       // towards +infinity: 101 to 200 is 200, -101 to 100 is -100
	RoundDown                     // towards -infinity: 199 to 100 is 100, -101 to 200 is -200
)

// RoundTo rounds to a multiple of step cents; see Exact.RoundTo. A step of 0 or less panics.
func (m Money) RoundTo(step int64, mode RoundMode) Money {
	return FromMoney(m).RoundTo(step, mode)
}

// Allocate splits amount by weights so the parts sum to m exactly: each part is its share rounded down and
// the leftover cents go to the largest remainders, the earlier part first. No weights, a negative
// weight or weights that sum to 0 give nil.
func Allocate(amount Money, weights ...int64) []Money {
	total := new(big.Int)

	for _, w := range weights {
		if w < 0 {
			return nil
		}

		total.Add(total, big.NewInt(w))
	}

	if total.Sign() == 0 {
		return nil
	}

	abs := new(big.Int).Abs(big.NewInt(amount.cents))
	parts := make([]int64, len(weights))
	rems := make([]*big.Int, len(weights))
	left := mustInt64(abs)

	for i, w := range weights {
		quo, rem := new(big.Int).QuoRem(new(big.Int).Mul(abs, big.NewInt(w)), total, new(big.Int))
		parts[i], rems[i] = mustInt64(quo), rem
		left -= parts[i]
	}

	order := make([]int, len(weights))
	for i := range order {
		order[i] = i
	}

	slices.SortStableFunc(order, func(a, b int) int { return rems[b].Cmp(rems[a]) })

	for _, i := range order[:left] {
		parts[i]++
	}

	out := make([]Money, len(parts))
	for i, p := range parts {
		if amount.cents < 0 {
			p = -p
		}

		out[i] = Money{cents: p, currency: amount.currency}
	}

	return out
}

// PercentOf is part/whole x 100, rounded half away from zero to decimals places and returned scaled
// by 10^decimals: 96.65% to 1 decimal is 967. ok is false when whole is zero or the currencies differ.
func PercentOf(part, whole Money, decimals int) (int64, bool) {
	if whole.cents == 0 {
		return 0, false
	}

	_, err := combine(part, whole)
	if err != nil {
		return 0, false
	}

	scale := pow10(max(decimals, 0))
	scaled := new(big.Int).Mul(big.NewInt(part.cents), big.NewInt(centsPerUnit))
	val := new(big.Rat).SetFrac(scaled, big.NewInt(whole.cents))

	return roundScaled(val, scale), true
}

func pow10(n int) *big.Int {
	return new(big.Int).Exp(big.NewInt(decimalBase), big.NewInt(int64(n)), nil)
}

// roundScaled is round(val x scale) half away from zero, as an int64.
func roundScaled(val *big.Rat, scale *big.Int) int64 {
	return mustInt64(roundRat(new(big.Rat).Mul(val, new(big.Rat).SetInt(scale))))
}

// roundRat rounds half away from zero.
func roundRat(rat *big.Rat) *big.Int {
	quo, rem := new(big.Int).QuoRem(new(big.Int).Abs(rat.Num()), rat.Denom(), new(big.Int))
	if rem.Lsh(rem, 1).Cmp(rat.Denom()) >= 0 {
		quo.Add(quo, big.NewInt(1))
	}

	if rat.Sign() < 0 {
		quo.Neg(quo)
	}

	return quo
}

// parseDecimal reads [-+]digits[.digits] exactly.
func parseDecimal(text string) (*big.Rat, error) {
	body := strings.TrimLeft(text, "+-")
	if len(text)-len(body) > 1 || body == "" {
		return nil, ErrDecimal
	}

	whole, frac, _ := strings.Cut(body, ".")
	if whole == "" && frac == "" || strings.ContainsFunc(whole+frac, func(c rune) bool { return c < '0' || c > '9' }) ||
		strings.Contains(body, ".") && frac == "" {
		return nil, ErrDecimal
	}

	digits, _ := new(big.Int).SetString(whole+frac, decimalBase)
	val := new(big.Rat).SetFrac(digits, pow10(len(frac)))

	if text[0] == '-' {
		val.Neg(val)
	}

	return val, nil
}
