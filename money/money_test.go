//nolint:gosec // random test data, not security
package money_test

import (
	"math"
	"math/big"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/money"
)

func eur(t *testing.T, s string) money.Money {
	t.Helper()

	m, err := money.FromDecimal(s, "EUR")
	require.NoError(t, err)

	return m
}

func TestMulRateTies(t *testing.T) {
	t.Parallel()

	for _, row := range []struct{ amount, pct, want string }{
		{"32726.50", "17", "5563.51"},
		{"19592.25", "6", "1175.54"},
		{"19592.25", "-6", "-1175.54"},
		{"-32726.50", "17", "-5563.51"},
		{"1.00", "0.5", "0.01"},
		{"-1.00", "0.5", "-0.01"},
		{"1.00", "0.49999", "0.00"},
		{"100", "2.61", "2.61"},
	} {
		rate, err := money.RateFromDecimal(row.pct)
		require.NoError(t, err)
		require.Equal(t, eur(t, row.want), eur(t, row.amount).MulRate(rate), "%s x %s%%", row.amount, row.pct)
	}
}

func TestChainedRoundsOnce(t *testing.T) {
	t.Parallel()

	net, err := eur(t, "23153.85").Sub(eur(t, "427.35"))
	require.NoError(t, err)
	require.Equal(t, eur(t, "3863.51"), net.MulRate(money.Percent(17)))

	// 100.05 x 1.95583 = 195.68079 and x 1.17 = 228.9465..., rounded once.
	step, err := money.FromMoney(eur(t, "100.05")).MulDecimal("1.95583")
	require.NoError(t, err)
	require.Equal(t, eur(t, "228.95"), step.AddRate(money.Percent(17)).Round())
}

func TestAddRemoveRate(t *testing.T) {
	t.Parallel()

	require.Equal(t, eur(t, "1170.00"), eur(t, "1000").AddRate(money.Percent(17)))
	require.Equal(t, eur(t, "1000.00"), eur(t, "1170").RemoveRate(money.Percent(17)))
}

func TestRatioAndDiv(t *testing.T) {
	t.Parallel()

	require.Equal(t, eur(t, "0.03"), eur(t, "0.05").MulRatio(1, 2))
	require.Equal(t, eur(t, "-0.03"), eur(t, "-0.05").MulRatio(1, 2))
	require.Equal(t, eur(t, "0.03"), eur(t, "0.10").DivInt(3))
}

func TestFromDecimal(t *testing.T) {
	t.Parallel()

	require.EqualValues(t, -50, eur(t, "-0.5").Cents())
	require.EqualValues(t, 1, eur(t, "0.005").Cents())
	require.EqualValues(t, -1, eur(t, "-0.005").Cents())
	require.EqualValues(t, 0, eur(t, "0.004").Cents())

	for _, bad := range []string{"", "-", ".", "1.", "1e3", "1,5", " 1", "--1", "1.2.3", "NaN", "0x10"} {
		_, err := money.FromDecimal(bad, "EUR")
		require.ErrorIs(t, err, money.ErrDecimal, bad)
	}
}

func TestFromFloatIsExactForEveryTwoDecimalValue(t *testing.T) {
	t.Parallel()

	require.EqualValues(t, 1999, money.FromFloat(19.99, "EUR").Cents())
	require.EqualValues(t, 30, money.FromFloat(0.1+0.2, "EUR").Cents())
	require.EqualValues(t, 99999999999999, money.FromFloat(999999999999.99, "EUR").Cents())
	require.EqualValues(t, -99999999999999, money.FromFloat(-999999999999.99, "EUR").Cents())
	require.True(t, money.FromFloat(0, "EUR").IsZero())

	for cents := int64(-3_000_000); cents < 3_000_000; cents += 7 {
		want := big.NewRat(cents, 100)
		f, _ := want.Float64()
		require.Equal(t, cents, money.FromFloat(f, "").Cents())
	}

	for range 20000 {
		cents := rand.Int64N(2_000_000_000_000_000) - 1_000_000_000_000_000
		f, _ := big.NewRat(cents, 100).Float64()
		require.Equal(t, cents, money.FromFloat(f, "").Cents(), cents)
	}
}

func TestStringAndFloat(t *testing.T) {
	t.Parallel()

	require.Equal(t, "32726.50 EUR", eur(t, "32726.5").String())
	require.Equal(t, "-0.05 EUR", money.New(-5, "EUR").String())
	require.Equal(t, "0.00", money.Money{}.String())
	require.InDelta(t, 12.34, money.New(1234, "").Float(), 0)
}

func TestCurrency(t *testing.T) {
	t.Parallel()

	_, err := money.New(1, "EUR").Add(money.New(1, "BAM"))
	require.ErrorIs(t, err, money.ErrCurrency)

	_, err = money.New(1, "EUR").Sub(money.New(1, "BAM"))
	require.ErrorIs(t, err, money.ErrCurrency)

	_, err = money.Sum(money.New(1, "EUR"), money.New(1, "BAM"))
	require.ErrorIs(t, err, money.ErrCurrency)

	got, err := money.New(1, "").Add(money.New(2, "EUR"))
	require.NoError(t, err)
	require.Equal(t, money.New(3, "EUR"), got)

	got, err = money.Sum()
	require.NoError(t, err)
	require.Equal(t, money.Money{}, got)

	_, err = money.New(1<<62, "").Add(money.New(1<<62, ""))
	require.ErrorIs(t, err, money.ErrOverflow)

	_, valid := money.PercentOf(money.New(1, "EUR"), money.New(1, "BAM"), 1)
	require.False(t, valid)
}

func TestNoOverflowAtLargeAmounts(t *testing.T) {
	t.Parallel()

	big := money.New(10_000_000_000_000, "EUR") // 1e13 cents
	require.EqualValues(t, 10_000_000_000_000, big.MulRate(money.Percent(100)).Cents())
	require.EqualValues(t, 20_000_000_000_000, big.AddRate(money.Percent(100)).Cents())
	require.EqualValues(t, 5_000_000_000_000, big.RemoveRate(money.Percent(100)).Cents())
}

func TestAllocate(t *testing.T) {
	t.Parallel()

	for _, row := range []struct {
		cents   int64
		weights []int64
		want    []int64
	}{
		{100, []int64{1, 1, 1}, []int64{34, 33, 33}},
		{-100, []int64{1, 1, 1}, []int64{-34, -33, -33}},
		{5, []int64{1, 1}, []int64{3, 2}},
		{100, []int64{0, 1}, []int64{0, 100}},
		{1, []int64{1, 1, 1}, []int64{1, 0, 0}},
		{
			10_000_000_000_000_000, []int64{3, 3, 3},
			[]int64{3_333_333_333_333_334, 3_333_333_333_333_333, 3_333_333_333_333_333},
		},
	} {
		got := money.Allocate(money.New(row.cents, "EUR"), row.weights...)

		cents := make([]int64, 0, len(got))
		for _, g := range got {
			cents = append(cents, g.Cents())
		}

		require.Equal(t, row.want, cents)
	}

	require.Nil(t, money.Allocate(money.New(1, ""), 0, 0))
	require.Nil(t, money.Allocate(money.New(1, "")))
	require.Nil(t, money.Allocate(money.New(1, ""), 1, -1))
}

func TestAllocateAlwaysSums(t *testing.T) {
	t.Parallel()

	for range 2000 {
		weights := make([]int64, 1+rand.IntN(6))
		for i := range weights {
			weights[i] = rand.Int64N(1000)
		}

		weights[0]++ // not all zero

		total := rand.Int64N(2_000_000) - 1_000_000

		var sum int64
		for _, part := range money.Allocate(money.New(total, "EUR"), weights...) {
			sum += part.Cents()
		}

		require.Equal(t, total, sum)
	}
}

func TestRoundTo(t *testing.T) {
	t.Parallel()

	for _, row := range []struct {
		cents, step int64
		mode        money.RoundMode
		want        int64
	}{
		{250, 500, money.RoundNearest, 500},
		{249, 500, money.RoundNearest, 0},
		{-250, 500, money.RoundNearest, -500},
		{-249, 500, money.RoundNearest, 0},
		{1234, 100, money.RoundNearest, 1200},
		{1250, 100, money.RoundNearest, 1300},
		{-1250, 100, money.RoundNearest, -1300},
		{101, 100, money.RoundUp, 200},
		{100, 100, money.RoundUp, 100},
		{-101, 100, money.RoundUp, -100},
		{199, 100, money.RoundDown, 100},
		{-101, 100, money.RoundDown, -200},
		{-100, 100, money.RoundDown, -100},
		{777, 0, money.RoundUp, 777},
	} {
		require.Equal(t, row.want, money.New(row.cents, "").RoundTo(row.step, row.mode).Cents(), "%+v", row)
	}
}

func TestPercentOf(t *testing.T) {
	t.Parallel()

	got, valid := money.PercentOf(eur(t, "14690.80"), eur(t, "15200"), 1)
	require.True(t, valid)
	require.EqualValues(t, 967, got)

	got, valid = money.PercentOf(eur(t, "-14690.80"), eur(t, "15200"), 1)
	require.True(t, valid)
	require.EqualValues(t, -967, got)

	got, _ = money.PercentOf(eur(t, "1"), eur(t, "3"), 0)
	require.EqualValues(t, 33, got)

	_, valid = money.PercentOf(eur(t, "1"), money.Money{}, 1)
	require.False(t, valid)
}

func TestRate(t *testing.T) {
	t.Parallel()

	rate, err := money.RateFromDecimal("2.61")
	require.NoError(t, err)
	require.EqualValues(t, 261000, rate.Units())
	require.EqualValues(t, 1700000, money.Percent(17).Units())
	require.EqualValues(t, 261000, money.RateFromFloat(2.61).Units())
	require.EqualValues(t, 1, money.RateFromFloat(0.000005).Units())
	require.InDelta(t, 2.61, rate.Float(), 0)

	_, err = money.RateFromDecimal("x")
	require.ErrorIs(t, err, money.ErrDecimal)
}

func TestExactAddAndZero(t *testing.T) {
	t.Parallel()

	var zero money.Exact
	require.Equal(t, money.Money{}, zero.Round())

	half := money.FromMoney(money.New(1, "EUR")).MulRatio(1, 2) // half a cent, kept
	sum, err := half.Add(half)
	require.NoError(t, err)
	require.EqualValues(t, 1, sum.Round().Cents())
	require.EqualValues(t, 1, half.Round().Cents())

	_, err = money.FromMoney(money.New(1, "EUR")).Add(money.FromMoney(money.New(1, "BAM")))
	require.ErrorIs(t, err, money.ErrCurrency)

	_, err = zero.MulDecimal("1,5")
	require.ErrorIs(t, err, money.ErrDecimal)
}

// reference is m x r/100 with the ratio kept in big.Rat and one half-away-from-zero rounding.
func reference(cents, units int64) int64 {
	x := new(big.Rat).Mul(big.NewRat(cents, 1), big.NewRat(units, 10_000_000))
	num, den := x.Num(), x.Denom()
	quo, rem := new(big.Int).QuoRem(new(big.Int).Abs(num), den, new(big.Int))

	if rem.Lsh(rem, 1).Cmp(den) >= 0 {
		quo.Add(quo, big.NewInt(1))
	}

	if num.Sign() < 0 {
		quo.Neg(quo)
	}

	return quo.Int64()
}

func FuzzMulRate(f *testing.F) {
	f.Add(int64(3272650), int64(1700000))
	f.Add(int64(-1959225), int64(600000))
	f.Add(int64(10_000_000_000_000), int64(10_000_000))
	f.Fuzz(func(t *testing.T, cents, units int64) {
		cents %= 1_000_000_000_000_000 // 1e13 of the major unit
		units %= 1_000_000_000         // 10,000 percent

		got := money.New(cents, "").MulRate(money.RateFromFloat(0)) // zero rate is zero
		require.Zero(t, got.Cents())

		rate, err := money.RateFromDecimal(rateText(units))
		require.NoError(t, err)
		require.Equal(t, units, rate.Units())
		require.Equal(t, reference(cents, units), money.New(cents, "").MulRate(rate).Cents())
	})
}

func rateText(units int64) string {
	sign := ""
	if units < 0 {
		sign, units = "-", -units
	}

	frac := big.NewInt(units % 100000).String()
	for len(frac) < 5 {
		frac = "0" + frac
	}

	return sign + big.NewInt(units/100000).String() + "." + frac
}

func TestMulRateMatchesReference(t *testing.T) {
	t.Parallel()

	for range 20000 {
		cents := rand.Int64N(2_000_000_000_000_000) - 1_000_000_000_000_000
		units := rand.Int64N(2_000_000_000) - 1_000_000_000

		rate, err := money.RateFromDecimal(rateText(units))
		require.NoError(t, err)
		require.Equal(t, reference(cents, units), money.New(cents, "").MulRate(rate).Cents(), "%d x %d", cents, units)
	}
}

func TestPanicsOnOverflow(t *testing.T) {
	t.Parallel()

	huge := money.New(math.MaxInt64, "")

	require.PanicsWithError(t, "money: overflow: 9223372036854775807000 does not fit in int64 cents",
		func() { huge.MulRate(money.Percent(100000)) })
	require.Panics(t, func() { huge.AddRate(money.Percent(100)) })
	require.Panics(t, func() { _, _ = money.FromDecimal("99999999999999999999", "") })
	require.Panics(t, func() { money.FromFloat(1e300, "") })
	require.Panics(t, func() { money.PercentOf(huge, money.New(1, ""), 2) })
	require.Panics(t, func() { money.Allocate(money.New(math.MinInt64, ""), 1, 1) })

	var err error

	func() {
		defer func() { err, _ = recover().(error) }()

		huge.MulRate(money.Percent(100000))
	}()
	require.ErrorIs(t, err, money.ErrOverflow)
}

func TestPanicsOnZeroDivisor(t *testing.T) {
	t.Parallel()

	one := money.New(100, "")

	require.PanicsWithValue(t, "money: MulRatio with a zero denominator divides by zero",
		func() { one.MulRatio(1, 0) })
	require.Panics(t, func() { one.DivInt(0) })
	require.Panics(t, func() { one.RemoveRate(money.Percent(-100)) })
	require.Panics(t, func() { money.FromMoney(one).RemoveRate(money.Percent(-100)) })
	require.Panics(t, func() { money.FromMoney(one).MulRatio(1, 0) })
}
