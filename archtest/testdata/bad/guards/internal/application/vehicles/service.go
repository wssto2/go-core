package vehicles

import domainvehicle "example.com/app/internal/domain/vehicle"

func change(v *domainvehicle.Vehicle) {
	v.Workflow.Phase = 2
	v.Status.Active = true
	v.Pricing.Note = "x"
	_ = domainvehicle.Pricing{ListPrice: 1}
}
