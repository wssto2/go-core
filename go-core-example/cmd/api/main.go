package main

import (
	"go-core-example/internal/domain/auth"
	"go-core-example/internal/domain/product"
	"log"

	"github.com/wssto2/go-core/bootstrap"
)

func main() {
	cfg := loadConfig()

	app, err := bootstrap.New[auth.User](cfg).
		DefaultInfrastructure().
		WithJWTAuth(auth.IdentityResolver).
		WithModules(product.NewModule()).
		Build()
	if err != nil {
		log.Fatal(err)
	}

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
