# Makefile
all: setup hooks

# requires `nvm use --lts` or `nvm use node`
.PHONY: setup
setup:
	npm install -g @commitlint/config-conventional @commitlint/cli

.PHONY: hooks
hooks:
	@git config --local core.hooksPath .githooks/

# Go targets
.PHONY: build
build:
	go build -o bin/rescuestream-api ./cmd/rescuestream-api

.PHONY: run
run:
	go run ./cmd/rescuestream-api

.PHONY: test
test:
	go test -v -race -coverprofile=coverage.out ./...

.PHONY: test-unit
test-unit:
	go test -v -race ./internal/...

.PHONY: lint
lint:
	golangci-lint run ./...

.PHONY: fmt
fmt:
	go fmt ./...
	goimports -w -local github.com/searchandrescuegg/rescuestream-api .

.PHONY: migrate
migrate:
	go run ./cmd/migrate up

.PHONY: migrate-down
migrate-down:
	go run ./cmd/migrate down

.PHONY: migrate-create
migrate-create:
	@read -p "Migration name: " name; \
	migrate create -ext sql -dir internal/database/migrations -seq $$name

.PHONY: clean
clean:
	rm -rf bin/ coverage.out

.PHONY: verify
verify: lint test