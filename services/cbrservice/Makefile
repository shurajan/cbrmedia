PKG      := ./cmd/cbrservice
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -s -w -X main.version=$(VERSION)
BUILD    ?= bin/

GOLANGCI := go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
VULN     := go run golang.org/x/vuln/cmd/govulncheck@latest

.DEFAULT_GOAL := help

.PHONY: help build run test cover lint fmt vet vuln check tidy clean

help: ## показать список целей
	@awk 'BEGIN {FS = ":.*## "} /^[a-z-]+:.*## / {printf "  %-8s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## собрать бинарники cbrservice и cbrconvert в bin/
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BUILD) ./cmd/...

run: ## запустить локально (данные в CBR_HOME, по умолчанию ~/.cbr)
	go run $(PKG) -log

test: ## тесты с race-детектором
	go test -race ./...

cover: ## покрытие тестами
	go test -race -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

lint: ## golangci-lint
	$(GOLANGCI) run ./...

fmt: ## отформатировать код (gofmt + goimports)
	$(GOLANGCI) fmt ./...

vet: ## go vet
	go vet ./...

vuln: ## проверка уязвимостей
	$(VULN) ./...

check: vet lint test vuln ## всё перед коммитом

tidy: ## go mod tidy
	go mod tidy

clean: ## удалить артефакты сборки
	rm -rf bin coverage.out
