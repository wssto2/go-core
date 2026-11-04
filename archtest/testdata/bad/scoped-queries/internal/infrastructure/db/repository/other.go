package repository

import "example.com/app/gen"

func (r *Vehicles) elsewhere() { r.db().Model(&gen.Vehicle{}) }
