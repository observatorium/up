#!/bin/bash

# This test spins up one Thanos receiver for ingestion and one querier for querying.
# The up binary is then run against them.

set -euo pipefail

result=1
trap 'kill $(jobs -p); exit $result' EXIT

(
  ./tmp/bin/thanos receive \
    --grpc-address=127.0.0.1:10901 \
    --http-address=127.0.0.1:10902 \
    --remote-write.address=127.0.0.1:19291 \
    --log.level=debug \
    --tsdb.path="$(mktemp -d)"
) &

(
  ./tmp/bin/thanos query \
    --grpc-address=127.0.0.1:10911 \
    --http-address=127.0.0.1:9091 \
    --store=127.0.0.1:10901 \
    --log.level=debug
) &

(
  ./tmp/bin/loki \
    -log.level=debug \
    -target=all \
    -config.file=./test/config/loki.yml
) &

echo "## waiting for dependencies to come up..."
sleep 10

if ./up \
  --listen=0.0.0.0:8888 \
  --endpoint-type=metrics \
  --endpoint-read=http://127.0.0.1:9091 \
  --endpoint-write=http://127.0.0.1:19291/api/v1/receive \
  --period=500ms \
  --initial-query-delay=250ms \
  --threshold=1 \
  --latency=10s \
  --duration=10s \
  --log.level=debug \
  --name=up_test \
  --labels='foo="bar"'; then
  result=0
  echo "## metrics tests: ok"
else
  result=1
  printf "## metrics tests: failed\n\n"
  exit 1
fi

if ./up \
  --listen=0.0.0.0:8888 \
  --endpoint-type=logs \
  --endpoint-read=http://127.0.0.1:3100/loki/api/v1/query \
  --endpoint-write=http://127.0.0.1:3100/loki/api/v1/push \
  --period=500ms \
  --initial-query-delay=250ms \
  --threshold=1 \
  --latency=10s \
  --duration=10s \
  --log.level=debug \
  --name=up_test \
  --labels='foo="bar"'\
  --logs="[\"$(date '+%s%N')\",\"log line 1\"]"; then
  result=0
  echo "## logs tests: ok"
else
  result=1
  printf "## logs tests: failed\n\n"
  exit 1
fi

printf "\t## all tests: ok\n\n" 1>&2
exit 0
