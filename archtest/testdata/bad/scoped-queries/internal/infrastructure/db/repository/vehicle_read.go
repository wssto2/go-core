package repository

import "example.com/app/gen"

type Vehicles struct{}

func (r *Vehicles) good() {
	r.scopedDB().Where("a").Model(&gen.Vehicle{})
	r.crossTenantDB("vin").Model(&gen.Vehicle{})
	r.db().Model(&gen.Dealer{})
}

func (r *Vehicles) bad() {
	r.db().Model(&gen.Vehicle{})
	r.db().Where("a").Model(&gen.Vehicle{})
}

func (r *Vehicles) local() {
	tx := r.scopedDB()
	tx.Model(&gen.Vehicle{})
}
