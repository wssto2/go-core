package order

type Update struct {
	Title string  `validation:"required"`
	Note  *string `validation:"max:5"`
	Where Place
	Tags  []string
}

func (Update) Validate() error { return nil }

type Place struct {
	Text string
}

type Filter struct{ Query string }
