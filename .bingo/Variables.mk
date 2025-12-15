# Auto generated binary variables helper managed by https://github.com/bwplotka/bingo v0.9. DO NOT EDIT.
# All tools are designed to be build inside $GOBIN.
BINGO_DIR := $(dir $(lastword $(MAKEFILE_LIST)))
GOPATH ?= $(shell go env GOPATH)
GOBIN  ?= $(firstword $(subst :, ,${GOPATH}))/bin
GO     ?= $(shell which go)

# Below generated variables ensure that every time a tool under each variable is invoked, the correct version
# will be used; reinstalling only if needed.
# For example for gojsontoyaml variable:
#
# In your main Makefile (for non array binaries):
#
#include .bingo/Variables.mk # Assuming -dir was set to .bingo .
#
#command: $(GOJSONTOYAML)
#	@echo "Running gojsontoyaml"
#	@$(GOJSONTOYAML) <flags/args..>
#
GOJSONTOYAML := $(GOBIN)/gojsontoyaml-v0.0.0-20200602132005-3697ded27e8c
$(GOJSONTOYAML): $(BINGO_DIR)/gojsontoyaml.mod
	@# Install binary/ries using Go 1.14+ build command. This is using bwplotka/bingo-controlled, separate go module with pinned dependencies.
	@echo "(re)installing $(GOBIN)/gojsontoyaml-v0.0.0-20200602132005-3697ded27e8c"
	@cd $(BINGO_DIR) && GOWORK=off $(GO) build -mod=mod -modfile=gojsontoyaml.mod -o=$(GOBIN)/gojsontoyaml-v0.0.0-20200602132005-3697ded27e8c "github.com/brancz/gojsontoyaml"

GOLANGCI_LINT := $(GOBIN)/golangci-lint-v1.45.2
$(GOLANGCI_LINT): $(BINGO_DIR)/golangci-lint.mod
	@# Install binary/ries using Go 1.14+ build command. This is using bwplotka/bingo-controlled, separate go module with pinned dependencies.
	@echo "(re)installing $(GOBIN)/golangci-lint-v1.45.2"
	@cd $(BINGO_DIR) && GOWORK=off $(GO) build -mod=mod -modfile=golangci-lint.mod -o=$(GOBIN)/golangci-lint-v1.45.2 "github.com/golangci/golangci-lint/cmd/golangci-lint"

JSONNET := $(GOBIN)/jsonnet-v0.16.0
$(JSONNET): $(BINGO_DIR)/jsonnet.mod
	@# Install binary/ries using Go 1.14+ build command. This is using bwplotka/bingo-controlled, separate go module with pinned dependencies.
	@echo "(re)installing $(GOBIN)/jsonnet-v0.16.0"
	@cd $(BINGO_DIR) && GOWORK=off $(GO) build -mod=mod -modfile=jsonnet.mod -o=$(GOBIN)/jsonnet-v0.16.0 "github.com/google/go-jsonnet/cmd/jsonnet"

JSONNETFMT := $(GOBIN)/jsonnetfmt-v0.16.0
$(JSONNETFMT): $(BINGO_DIR)/jsonnetfmt.mod
	@# Install binary/ries using Go 1.14+ build command. This is using bwplotka/bingo-controlled, separate go module with pinned dependencies.
	@echo "(re)installing $(GOBIN)/jsonnetfmt-v0.16.0"
	@cd $(BINGO_DIR) && GOWORK=off $(GO) build -mod=mod -modfile=jsonnetfmt.mod -o=$(GOBIN)/jsonnetfmt-v0.16.0 "github.com/google/go-jsonnet/cmd/jsonnetfmt"

KUBEVAL := $(GOBIN)/kubeval-v0.0.0-20201005082916-38668c6c5b23
$(KUBEVAL): $(BINGO_DIR)/kubeval.mod
	@# Install binary/ries using Go 1.14+ build command. This is using bwplotka/bingo-controlled, separate go module with pinned dependencies.
	@echo "(re)installing $(GOBIN)/kubeval-v0.0.0-20201005082916-38668c6c5b23"
	@cd $(BINGO_DIR) && GOWORK=off $(GO) build -mod=mod -modfile=kubeval.mod -o=$(GOBIN)/kubeval-v0.0.0-20201005082916-38668c6c5b23 "github.com/instrumenta/kubeval"

MDOX := $(GOBIN)/mdox-v0.9.0
$(MDOX): $(BINGO_DIR)/mdox.mod
	@# Install binary/ries using Go 1.14+ build command. This is using bwplotka/bingo-controlled, separate go module with pinned dependencies.
	@echo "(re)installing $(GOBIN)/mdox-v0.9.0"
	@cd $(BINGO_DIR) && GOWORK=off $(GO) build -mod=mod -modfile=mdox.mod -o=$(GOBIN)/mdox-v0.9.0 "github.com/bwplotka/mdox"

