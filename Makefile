BINARY  := vigil
PKG     := github.com/bc0d3/vigil
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
	-X main.version=$(VERSION) \
	-X main.commit=$(COMMIT) \
	-X main.date=$(DATE)

.DEFAULT_GOAL := build

.PHONY: build
build: ## Compila el binario estático
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/vigil

.PHONY: install
install: ## Instala el binario en GOBIN
	CGO_ENABLED=0 go install -trimpath -ldflags "$(LDFLAGS)" ./cmd/vigil

.PHONY: test
test: ## Corre los tests con race detector y cobertura
	go test -race -cover ./...

.PHONY: cover
cover: ## Genera reporte de cobertura HTML
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out

.PHONY: vet
vet: ## go vet
	go vet ./...

.PHONY: fmt
fmt: ## Formatea el código
	gofmt -w .

.PHONY: lint
lint: ## Corre golangci-lint (debe estar instalado)
	golangci-lint run

.PHONY: tidy
tidy: ## Ordena go.mod
	go mod tidy

.PHONY: docker
docker: ## Construye la imagen Docker
	docker build --build-arg VERSION=$(VERSION) -t $(BINARY):$(VERSION) -t $(BINARY):latest .

.PHONY: snapshot
snapshot: ## Release local de prueba con GoReleaser (no publica)
	goreleaser release --snapshot --clean

.PHONY: clean
clean: ## Limpia artefactos
	rm -rf $(BINARY) dist coverage.out

.PHONY: check
check: fmt vet test ## fmt + vet + test

.PHONY: help
help: ## Muestra esta ayuda
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'
