package money

import (
	"math/big"
	"strconv"
)

// rateScale is the units of a Rate in one percent; a hundred percent is rateScale x 100.
const rateScale = 100_000

// Rate is a percent in units of 1/100,000 of a percent, the scale of a decimal(10,5) percent column:
// 2.61000% is 261000 and 17% is 1700000. It is comparable; the zero value is 0%.
type Rate struct{ v int64 }

// Percent is a whole percent: Percent(17) is 17%.
func Percent(whole int64) Rate { return Rate{v: whole * rateScale} }

// RateFromDecimal parses an exact decimal percent such as "17" or "2.61". More than 5 decimals round
// half away from zero.
func RateFromDecimal(s string) (Rate, error) {
	val, err := parseDecimal(s)
	if err != nil {
		return Rate{}, err
	}

	return Rate{v: roundScaled(val, big.NewInt(rateScale))}, nil
}

// RateFromFloat reads a percent float64 that came from the database, exact like FromFloat; for the
// database edge only. NaN and infinities give 0%.
func RateFromFloat(f float64) Rate {
	rate, _ := RateFromDecimal(strconv.FormatFloat(f, 'f', -1, 64))

	return rate
}

// Units is the rate in 1/100,000 of a percent.
func (r Rate) Units() int64 { return r.v }

// Float is the percent as a float64, for writing back to the database or JSON only.
func (r Rate) Float() float64 { return float64(r.v) / rateScale }

// ratio is r/100 as an exact fraction.
func (r Rate) ratio() *big.Rat { return big.NewRat(r.v, rateScale*centsPerUnit) }
