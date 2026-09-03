.PHONY: run build tidy vet test docker-up docker-down migrate-up clean

APP_NAME=url-shortener
BIN=bin/server

run:
	go run ./cmd/server

build:
	go build -o $(BIN) ./cmd/server

tidy:
	go mod tidy

vet:
	go vet ./...

test:
	go test ./... -v

docker-up:
	docker compose up --build -d

docker-down:
	docker compose down -v

logs:
	docker compose logs -f app

migrate-up:
	docker compose exec postgres psql -U postgres -d urlshortener -f /migrations/000001_create_urls.up.sql || echo "Run via app auto-migrate or psql manually"

clean:
	rm -rf bin/
