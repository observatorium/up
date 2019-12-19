all: build

build: up

.PHONY: up
up:
	CGO_ENABLED=0 go build -v -ldflags '-w -extldflags '-static''

container: Dockerfile up
	docker build -t quay.io/observatorium/up:latest .
