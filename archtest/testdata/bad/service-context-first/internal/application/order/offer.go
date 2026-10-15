package order

type OfferAppService struct{}

func (OfferAppService) Do(id int) {}

func (OfferAppService) hidden(id int) {}

type Other struct{}

func (Other) Do(id int) {}
