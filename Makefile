.PHONY: help dependencies build-linux-amd build-linux-arm build-osx-arm build-windows clean test vet


# Variables
VERSION ?= $(shell git describe --tags --always --dirty)
LDFLAGS=-ldflags "-w -s -X git-exporter/internal/cli.Version=$(VERSION)"

# Colors for output
BLUE = \033[0;34m
GREEN = \033[0;32m
RED = \033[0;31m
NC = \033[0m # No Color

help: ## Display help
	@echo "$(BLUE)Tock Helm Chart - Makefile$(NC)"
	@echo ""
	@echo "$(GREEN)Commands available:$(NC)"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  $(BLUE)%-20s$(NC) %s\n", $$1, $$2}'

dependencies: ## get dependencies
	@echo "$(BLUE)Get dependencies$(NC)"
	go mod tidy
	@echo "$(GREEN)Dependencies has been updated$(NC)"

build-linux-amd: dependencies ## build linux amd version
	@echo "$(BLUE)Build linux amd version$(NC)"
	GOOS=linux GOARCH=amd64 go build -a -v ${LDFLAGS} -o bin/linux/git-exporter
	@echo "$(BLUE)✓ Build completed, linux amd version is located in bin/linux/ folder.$(NC)"

build-linux-arm: dependencies ## build linux arm version
	@echo "$(BLUE)Build linux arm version$(NC)"
	GOOS=linux GOARCH=arm64 go build -a -v ${LDFLAGS} -o bin/linux-arm/git-exporter
	@echo "$(BLUE)✓ Build completed, linux arm version is located in bin/linux-arm/ folder.$(NC)"

build-osx-arm: dependencies ## build osx arm version
	@echo "$(BLUE)Build osx arm version$(NC)"
	GOOS=darwin GOARCH=arm64 go build -a -v ${LDFLAGS} -o bin/osx-silicon/git-exporter
	@echo "$(BLUE)✓ Build completed, osx arm version is located in bin/osx-silicon/ folder.$(NC)"

build-windows: dependencies## build windows version
	@echo "$(BLUE)Build windows version$(NC)"
	GOOS=windows GOARCH=amd64 go build -a -v ${LDFLAGS} -o bin/windows/git-exporter.exe
	@echo "$(BLUE)✓ Build completed, windows version is located in bin/windows/ folder.$(NC)"

clean: ## clean
	@echo "$(BLUE)Clean$(NC)"
	go clean
	@echo "$(BLUE)✓ Clean completed$(NC)"

test: ## run tests
	@echo "$(BLUE)Run go test ...$(NC)"
	go test -v ./...
	@echo "$(BLUE)✓ Tests completed$(NC)"

vet: ## Check code, use go vet
	@echo "$(BLUE)Check code ...$(NC)"
	go vet ./...
	@echo "$(BLUE)✓ code analyse completed$(NC)"
