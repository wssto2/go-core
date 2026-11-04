package repository

type Handle struct{}

func (h *Handle) DB() *Handle { return h }
