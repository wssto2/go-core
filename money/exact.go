package money

import "math/big"

// Exact is an amount in cents kept as an exact fraction, so a chain of steps loses nothing and is
// rounded once, by Round. The zero value is 0. An Exact is immutable.
type Exact struct {
	r        *big.Rat
	currency string
}

// FromMoney starts a chain from an amount.
func FromMoney(m Money) Exact { return Exact{r: big.NewRat(m.cents, 1), currency: m.currency} }

// MulRate is e x r / 100.
func (e Exact) MulRate(r Rate) Exact { return e.with(new(big.Rat).Mul(e.val(), r.ratio())) }

// AddRate is e x (1 + r/100).
func (e Exact) AddRate(r Rate) Exact {
	return e.with(new(big.Rat).Mul(e.val(), new(big.Rat).Add(big.NewRat(1, 1), r.ratio())))
}

// RemoveRate is e / (1 + r/100). A rate of -100% panics (division by zero).
func (e Exact) RemoveRate(r Rate) Exact {
	gross := new(big.Rat).Add(big.NewRat(1, 1), r.ratio())
	if gross.Sign() == 0 {
		panic("money: RemoveRate of -100% divides by zero")
	}

	return e.with(new(big.Rat).Quo(e.val(), gross))
}

// MulRatio is e x num / den. A zero den panics, like Go's integer division.
func (e Exact) MulRatio(num, den int64) Exact {
	if den == 0 {
		panic("money: MulRatio with a zero denominator divides by zero")
	}

	return e.with(new(big.Rat).Mul(e.val(), big.NewRat(num, den)))
}

// MulDecimal is e x an exact decimal such as an exchange rate, "1.95583".
func (e Exact) MulDecimal(s string) (Exact, error) {
	val, err := parseDecimal(s)
	if err != nil {
		return Exact{}, err
	}

	return e.with(val.Mul(val, e.val())), nil
}

// Add is e + other; different currencies are ErrCurrency (an empty one combines with any).
func (e Exact) Add(other Exact) (Exact, error) {
	cur, err := combine(Money{cents: 0, currency: e.currency}, Money{cents: 0, currency: other.currency})
	if err != nil {
		return Exact{}, err
	}

	return Exact{r: new(big.Rat).Add(e.val(), other.val()), currency: cur}, nil
}

// Round is the one rounding, half away from zero, to cents. It panics with an error wrapping
// ErrOverflow when the value does not fit int64 cents (about 9.2e16 of the major unit).
func (e Exact) Round() Money {
	return Money{cents: mustInt64(roundRat(e.val())), currency: e.currency}
}

func (e Exact) val() *big.Rat {
	if e.r == nil {
		return new(big.Rat)
	}

	return e.r
}

func (e Exact) with(r *big.Rat) Exact { return Exact{r: r, currency: e.currency} }
