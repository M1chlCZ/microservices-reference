.PHONY: build test vet fmt tidy proto up down logs smoke

build:
	go build ./...

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l .

tidy:
	go mod tidy

proto:
	protoc -I . \
	  --go_out=. --go_opt=module=github.com/m1chlcz/microservices-reference \
	  --go-grpc_out=. --go-grpc_opt=module=github.com/m1chlcz/microservices-reference \
	  api/events/v1/events.proto api/inventory/v1/inventory.proto api/payments/v1/payments.proto

up:
	docker compose up --build -d

down:
	docker compose down

logs:
	docker compose logs -f

smoke:
	./scripts/smoke.sh
