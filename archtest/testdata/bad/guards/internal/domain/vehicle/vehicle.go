package vehicle

type Pricing struct {
	ListPrice int
	Note      string
}

type Workflow struct{ Phase int }

type Vehicle struct {
	Workflow Workflow
	Pricing  Pricing
}

func (v *Vehicle) transitionPhase(phase int) { v.Workflow.Phase = phase }

func (v *Vehicle) Sneak(phase int) { v.Workflow.Phase = phase }

func (v *Vehicle) ChangePrices() {
	v.Pricing.ListPrice = 1
	_ = Pricing{ListPrice: 2}
}

func (v *Vehicle) Other() {
	v.Pricing.Note = "x"
	v.Pricing.ListPrice++
	_ = Pricing{ListPrice: 3}
}
