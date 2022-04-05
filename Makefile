include .bingo/Variables.mk

IMAGE?=quay.io/observatorium/up
TAG?=$(shell echo "$(shell git rev-parse --abbrev-ref HEAD | tr / -)-$(shell date +%Y-%m-%d)-$(shell git rev-parse --short HEAD)")

BIN_DIR ?= ./tmp/bin
THANOS=$(BIN_DIR)/thanos
LOKI ?= $(BIN_DIR)/loki
LOKI_VERSION ?= 1.5.0

EXAMPLES := examples
MANIFESTS := ${EXAMPLES}/manifests

all: build generate validate

build: up

.PHONY: up
up: vendor
	CGO_ENABLED=0 go build -v -ldflags '-w -extldflags '-static'' ./cmd/up

.PHONY: generate
generate: jsonnet-fmt ${MANIFESTS} README.md

.PHONY: validate
validate: $(KUBEVAL) $(MANIFESTS)
	$(KUBEVAL) --ignore-missing-schemas $(MANIFESTS)/*.yaml

.PHONY: vendor
vendor: go.mod go.sum
	go mod tidy
	go mod vendor

.PHONY: go-fmt
go-fmt:
	@fmt_res=$$(gofmt -d -s $$(find . -type f -name '*.go' -not -path './vendor/*' -not -path './jsonnet/vendor/*')); if [ -n "$$fmt_res" ]; then printf '\nGofmt found style issues. Please check the reported issues\nand fix them if necessary before submitting the code for review:\n\n%s' "$$fmt_res"; exit 1; fi

.PHONY: lint
lint: $(GOLANGCI_LINT)
	$(GOLANGCI_LINT) run -v -c .golangci.yml

image:
	docker build -t $(IMAGE):$(TAG) .

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

.PHONY: test
test:
	CGO_ENABLED=1 go test -v -race ./...

.PHONY: test-integration
test-integration: build test/integration.sh | $(THANOS) $(LOKI)
	PATH=$$PATH:$$(pwd)/$(BIN_DIR) ./test/integration.sh

.PHONY: ${MANIFESTS}
${MANIFESTS}: jsonnet/main.jsonnet jsonnet/*.libsonnet $(JSONNET) $(GOJSONTOYAML)
	@rm -rf ${MANIFESTS}
	@mkdir -p ${MANIFESTS}
	$(JSONNET) -J jsonnet/vendor -m ${MANIFESTS} jsonnet/main.jsonnet | xargs -I{} sh -c 'cat {} | $(GOJSONTOYAML) > {}.yaml && rm -f {}' -- {}

JSONNET_SRC = $(shell find . -name 'vendor' -prune -o -name 'examples/vendor' -prune -o -name 'tmp' -prune -o -name '*.libsonnet' -print -o -name '*.jsonnet' -print)
JSONNETFMT_CMD := $(JSONNETFMT) -n 2 --max-blank-lines 2 --string-style s --comment-style s

.PHONY: jsonnet-fmt
jsonnet-fmt: | $(JSONNETFMT)
	PATH=$$PATH:$(BIN_DIR):$(FIRST_GOPATH)/bin echo ${JSONNET_SRC} | xargs -n 1 -- $(JSONNETFMT_CMD) -i

.PHONY: format
format: $(GOLANGCI_LINT) go-fmt jsonnet-fmt
	$(GOLANGCI_LINT) run --fix -c .golangci.yml

$(BIN_DIR):
	mkdir -p $(BIN_DIR)

$(THANOS): $(BIN_DIR)
	wget -O ./tmp/thanos.tar.gz https://github.com/thanos-io/thanos/releases/download/v0.11.0/thanos-0.11.0.linux-amd64.tar.gz
	tar xvfz ./tmp/thanos.tar.gz -C ./tmp
	mv ./tmp/thanos-0.11.0.linux-amd64/thanos $@

$(LOKI): $(BIN_DIR)
	loki_pkg="loki-$$(go env GOOS)-$$(go env GOARCH)" && \
	cd $(BIN_DIR) && curl -O -L "https://github.com/grafana/loki/releases/download/v$(LOKI_VERSION)/$$loki_pkg.zip" && \
	unzip $$loki_pkg.zip && \
	mv $$loki_pkg loki && \
	rm $$loki_pkg.zip
