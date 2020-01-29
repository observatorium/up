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

echo "## waiting for dependencies to come up..."
sleep 5

if ./up \
  --listen=0.0.0.0:8888 \
  --endpoint-read=http://127.0.0.1:9091/api/v1/query \
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
  echo "## tests: ok"
  exit 0
fi

echo "## tests: failed" 1>&2
result=1
exit 1
