FROM alpine

COPY up /usr/bin/up

ENTRYPOINT ["/usr/bin/up"]
