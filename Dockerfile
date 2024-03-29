FROM golang:1.21-alpine3.18 as builder

RUN apk add ca-certificates --no-cache make && update-ca-certificates

WORKDIR /workspace

COPY . .

RUN make build

FROM scratch

COPY --from=builder /workspace/up /usr/bin/up
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

ENTRYPOINT ["/usr/bin/up"]
