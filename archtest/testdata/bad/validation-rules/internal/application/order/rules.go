package order

type Rules struct {
	Count int    `validation:"gte=0"`
	Name  string `validation:"required|max:3"`
}
