package crm

// Another package's Pricing is not the vehicle's.
type Pricing struct{ ListPrice int }

var _ = Pricing{ListPrice: 1}
