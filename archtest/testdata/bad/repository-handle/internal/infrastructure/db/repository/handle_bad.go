package repository

type Bad struct{ conn *Handle }

func (r *Bad) db() *Handle { return r.conn }

func (r *Bad) find() (*Bad, error) {
	return nil, nil //nolint:nilnil // not found is not an error here
}

func (r *Bad) fine() (*Bad, error) {
	return nil, nil //nolint:gosec
}
