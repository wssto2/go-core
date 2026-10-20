package pricing

type Offer struct {
	NetPrice   float64
	UnitPrice  *float64
	Totals     []float64
	VATAmount  float64
	LegacyFee  float64 // exempted in the test
	Network    float64
	Valuer     float64
	Percent    float64
	NetworkID  int64
	Cents      int64
	Discounted float64
}

type Calculator interface {
	Quote(basePrice float64, count int) (grossTotal float64, err error)
}

func Round(amount float64, step float64) float64 { return amount }

func (o *Offer) Apply(discount float64) (rate float64) { return discount }

func Mean(values []float64) float64 { return 0 }
