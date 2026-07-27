.PHONY: fmt test build run-api run-cli run-controller demo demo-full demo-failures validate-service compose-up compose-down clean

fmt:
	go fmt ./...

test:
	go test ./...

build:
	mkdir -p bin
	go build -trimpath -o bin/attesta-api ./api/cmd/attesta-api
	go build -trimpath -o bin/attesta ./cli/cmd/attesta
	go build -trimpath -o bin/attesta-controller ./controller/cmd/attesta-controller

run-api:
	ATTESTA_API_TOKEN=local-dev-token go run ./api/cmd/attesta-api

run-cli:
	ATTESTA_API_TOKEN=local-dev-token go run ./cli/cmd/attesta

run-controller:
	go run ./controller/cmd/attesta-controller

demo:
	bash scripts/demo-local.sh

demo-full:
	bash scripts/local-ci-cd.sh

demo-failures:
	bash scripts/failure-mode-demo.sh

validate-service:
	bash scripts/validate-service-scaffold.sh generated/services/payments-api

compose-up:
	docker compose up --build

compose-down:
	docker compose down

clean:
	rm -rf generated
