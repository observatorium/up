BIN_DIR ?= ./tmp/bin
EMBEDMD ?= $(BIN_DIR)/embedmd
THANOS=$(BIN_DIR)/thanos

FIRST_GOPATH := $(firstword $(subst :, ,$(shell go env GOPATH)))
GOLANGCILINT ?= $(FIRST_GOPATH)/bin/golangci-lint
GOLANGCILINT_VERSION ?= v1.21.0

all: build

build: up

.PHONY: up
up:
	CGO_ENABLED=0 go build -v -ldflags '-w -extldflags '-static'' ./cmd/up

.PHONY: vendor
vendor: go.mod go.sum
	go mod tidy
	go mod vendor

.PHONY: format
format: $(GOLANGCILINT) go-fmt
	$(GOLANGCILINT) run --fix --enable-all -c .golangci.yml

.PHONY: go-fmt
go-fmt:
	@fmt_res=$$(gofmt -d -s $$(find . -type f -name '*.go' -not -path './vendor/*' -not -path './jsonnet/vendor/*')); if [ -n "$$fmt_res" ]; then printf '\nGofmt found style issues. Please check the reported issues\nand fix them if necessary before submitting the code for review:\n\n%s' "$$fmt_res"; exit 1; fi

.PHONY: lint
lint: $(GOLANGCILINT)
	$(GOLANGCILINT) run -v --enable-all -c .golangci.yml

container: Dockerfile up
	docker build -t quay.io/observatorium/up:latest .

.PHONY: clean
clean:
	-rm tmp/help.txt
	-rm ./up

tmp/help.txt: clean build
	mkdir -p tmp
	-./up --help >tmp/help.txt 2>&1

.PHONY: README.md
README.md: $(EMBEDMD) tmp/help.txt
	$(EMBEDMD) -w README.md

.PHONY: test-integration
test-integration: build test/integration.sh | $(THANOS)
	PATH=$$PATH:$$(pwd)/$(BIN_DIR) ./test/integration.sh

$(BIN_DIR):
	mkdir -p $(BIN_DIR)

$(EMBEDMD): vendor $(BIN_DIR)
	go build -mod=vendor -o $@ github.com/campoy/embedmd

$(THANOS): $(BIN_DIR)
	wget -O ./tmp/thanos.tar.gz https://github.com/thanos-io/thanos/releases/download/v0.11.0/thanos-0.11.0.linux-amd64.tar.gz
	tar xvfz ./tmp/thanos.tar.gz -C ./tmp
	mv ./tmp/thanos-0.11.0.linux-amd64/thanos $@

$(GOLANGCILINT):
	curl -sfL https://raw.githubusercontent.com/golangci/golangci-lint/$(GOLANGCILINT_VERSION)/install.sh \
		| sed -e '/install -d/d' \
		| sh -s -- -b $(FIRST_GOPATH)/bin $(GOLANGCILINT_VERSION)
