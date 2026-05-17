.PHONY: fmt test build run-api run-cli run-controller demo demo-full demo-failures validate-service compose-up compose-down clean

fmt:
	go fmt ./...

test:
	go test ./...

build:
	go build ./api/cmd/sentinel-api
	go build ./cli/cmd/sentinel
	go build ./controller/cmd/sentinel-controller

run-api:
	SENTINEL_API_TOKEN=local-dev-token go run ./api/cmd/sentinel-api

run-cli:
	SENTINEL_API_TOKEN=local-dev-token go run ./cli/cmd/sentinel

run-controller:
	go run ./controller/cmd/sentinel-controller

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
