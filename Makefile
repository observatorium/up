FIRST_GOPATH := $(firstword $(subst :, ,$(shell go env GOPATH)))
EMBEDMD ?= $(FIRST_GOPATH)/bin/embedmd

all: build

build: up

.PHONY: up
up:
	CGO_ENABLED=0 go build -v -ldflags '-w -extldflags '-static''

container: Dockerfile up
	docker build -t quay.io/observatorium/up:latest .

.PHONY: clean
clean:
	-rm tmp/help.txt
	-rm ./up

tmp/help.txt: clean build
	mkdir -p tmp
	-./up --help &> tmp/help.txt

.PHONY: README.md
README.md: $(EMBEDMD) tmp/help.txt
	$(EMBEDMD) -w README.md

$(EMBEDMD):
	GO111MODULE=off go get -u github.com/campoy/embedmd
