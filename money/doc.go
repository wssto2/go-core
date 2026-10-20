// Package money calculates with money on integer cents, never on float64.
//
// A Money is an amount in the smallest unit (cents) and a currency code; a Rate is a percent. Every
// calculation is exact integer arithmetic with ONE rounding at the end, half away from zero (the
// meaning of math.Round and of PHP round()), so 32726.50 x 17% is 5563.51, not the 5563.50 a float
// gives for 5563.50499....
//
// # Using it at the edge
//
// A database column or a JSON number is a float64 or a decimal; the calculation is not. Read the
// column into Money in the mapper, calculate in Money, and write back with Float:
//
//	net := money.FromFloat(row.NetPrice, "EUR")        // mapper, reading
//	vat := net.MulRate(money.RateFromFloat(row.VatPct)) // calculation
//	row.Vat = vat.Float()                               // mapper, writing
//
// A calculation that chains steps (a sum x an exchange rate x (1 + customs%) + costs) keeps the
// exact intermediates in an Exact and rounds once, where the application decides:
//
//	total := money.FromMoney(sum).MulDecimal("1.95583").AddRate(customs).Round()
//
// Money has no JSON or database methods on purpose: the application's DTOs keep plain numbers.
package money
