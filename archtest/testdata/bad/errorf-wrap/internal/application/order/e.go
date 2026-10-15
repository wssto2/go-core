package order

import "fmt"

func wrap(err error) error { return fmt.Errorf("x: %v", err) }
