package resource

// Map transforms a Response[TFrom] into a Response[TTo] by applying fn to the
// Data field. The Meta map is carried over unchanged.
//
// Use this in repository methods that fetch a DB entity and need to return a
// domain type without duplicating the meta-preservation boilerplate:
//
//	resp, err := resource.New[entities.Locale](db).FindByID(ctx, id)
//	if err != nil { return resource.Response[Locale]{}, err }
//	return resource.Map(resp, LocaleFromRow), nil
func Map[TFrom, TTo any](r Response[TFrom], fn func(TFrom) TTo) Response[TTo] {
	return Response[TTo]{
		Data: fn(r.Data),
		Meta: r.Meta,
	}
}
