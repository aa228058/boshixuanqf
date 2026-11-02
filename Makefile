.PHONY: build run test migrate-up migrate-down docker-up docker-down clean

APP_NAME=tg-sender
BUILD_DIR=./build

build:
	CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o $(BUILD_DIR)/$(APP_NAME) ./cmd/server

run:
	go run ./cmd/server

test:
	go test -v ./...

migrate-up:
	goose -dir migrations postgres "$${DATABASE_URL:-host=localhost port=5432 user=postgres password=postgres dbname=tg_sender sslmode=disable}" up

migrate-down:
	goose -dir migrations postgres "$${DATABASE_URL:-host=localhost port=5432 user=postgres password=postgres dbname=tg_sender sslmode=disable}" down

docker-up:
	docker-compose up -d --build

docker-down:
	docker-compose down -v

docker-logs:
	docker-compose logs -f app

clean:
	rm -rf $(BUILD_DIR)
	go clean

deps:
	go mod download
	go mod tidy

fmt:
	go fmt ./...

vet:
	go vet ./...

lint: fmt vet
	golangci-lint run
