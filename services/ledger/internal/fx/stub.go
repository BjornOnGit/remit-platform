package fx

import (
	"errors"

	"github.com/shopspring/decimal"
)

// StubSource is stored on transactions while rates come from the fixed table below.
// Replaced by the FX Service in Task 4.5.
const StubSource = "stub"

var ErrRateUnavailable = errors.New("fx rate unavailable for this currency pair")

// Units of each currency per 1 USD (stub data).
var perUSD = map[string]decimal.Decimal{
	"USD": decimal.NewFromInt(1),
	"NGN": decimal.NewFromInt(1500),
	"EUR": decimal.RequireFromString("0.92"),
	"GBP": decimal.RequireFromString("0.79"),
}

// Rate returns how many units of `to` one unit of `from` buys.
func Rate(from, to string) (decimal.Decimal, error) {
	f, okFrom := perUSD[from]
	t, okTo := perUSD[to]
	if !okFrom || !okTo {
		return decimal.Zero, ErrRateUnavailable
	}
	return t.DivRound(f, 12), nil
}