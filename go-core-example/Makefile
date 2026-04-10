.PHONY: run build test vet up down logs

## run: start the API server (development mode)
run:
	go run ./cmd/api/

## build: compile the binary to ./bin/api
build:
	mkdir -p bin
	go build -o bin/api ./cmd/api/

## test: run all tests
test:
	go test ./...

## vet: run go vet
vet:
	go vet ./...

## up: build and start all Docker services (API + MySQL + Prometheus)
up:
	docker compose up --build -d

## down: stop and remove all Docker services
down:
	docker compose down

## logs: tail logs from all Docker services
logs:
	docker compose logs -f
