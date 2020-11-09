include .bingo/Variables.mk

BIN_DIR ?= ./tmp/bin
THANOS=$(BIN_DIR)/thanos
LOKI ?= $(BIN_DIR)/loki
LOKI_VERSION ?= 1.5.0

EXAMPLES := examples
MANIFESTS := ${EXAMPLES}/manifests/

all: build generate

build: up

.PHONY: up
up: go-vendor
	CGO_ENABLED=0 go build -v -ldflags '-w -extldflags '-static'' ./cmd/up

.PHONY: generate
generate: jsonnet-vendor jsonnet-fmt ${MANIFESTS} README.md

.PHONY: go-vendor
go-vendor: go.mod go.sum
	go mod tidy
	go mod vendor

.PHONY: go-fmt
go-fmt:
	@fmt_res=$$(gofmt -d -s $$(find . -type f -name '*.go' -not -path './vendor/*' -not -path './jsonnet/vendor/*')); if [ -n "$$fmt_res" ]; then printf '\nGofmt found style issues. Please check the reported issues\nand fix them if necessary before submitting the code for review:\n\n%s' "$$fmt_res"; exit 1; fi

.PHONY: lint
lint: $(GOLANGCI_LINT)
	$(GOLANGCI_LINT) run -v -c .golangci.yml

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
test-integration: build test/integration.sh | $(THANOS) $(LOKI)
	PATH=$$PATH:$$(pwd)/$(BIN_DIR) ./test/integration.sh

JSONNET_SRC = $(shell find . -name 'vendor' -prune -o -name '*.libsonnet' -print -o -name '*.jsonnet' -print)

.PHONY: jsonnet-vendor
jsonnet-vendor: jsonnet/jsonnetfile.json $(JB)
	rm -rf jsonnet/vendor
	cd jsonnet && $(JB) install

.PHONY: ${MANIFESTS}
${MANIFESTS}: jsonnet/main.jsonnet jsonnet/lib/* $(JSONNET) $(GOJSONTOYAML)
	@rm -rf ${MANIFESTS}
	@mkdir -p ${MANIFESTS}
	$(JSONNET) -J jsonnet/vendor -m ${MANIFESTS} jsonnet/main.jsonnet | xargs -I{} sh -c 'cat {} | $(GOJSONTOYAML) > {}.yaml && rm -f {}' -- {}

.PHONY: jsonnet-fmt
jsonnet-fmt: $(JSONNETFMT)
	@echo ${JSONNET_SRC} | xargs -n 1 -- $(JSONNETFMT) -n 2 --max-blank-lines 2 --string-style s --comment-style s -i

.PHONY: vendor
vendor: go-vendor jsonnet-vendor

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
