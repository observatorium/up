# UP

UP is a simple client that makes Prometheus remote-write requests.
The client writes the specified metric with the value `1` at a chosen interval.

[![Build Status](https://cloud.drone.io/api/badges/observatorium/up/status.svg)](https://cloud.drone.io/observatorium/up)

## Getting Started

The easiest way to begin making remote write requests is to run the UP container.
For example, to report an `up` metric every 10 seconds, run:

```shell
docker run --rm -p 8080 quay.io/observatorium/up --endpoint=https://example.com/api/v1/receive --period=10s --labels id='"'$(uuidgen)'"'
```

Note that the metric name and labels are customizable.
For example, to report a metric named `foo` with a custom `bar` label, run:

```shell
docker run --rm -p 8080 quay.io/observatorium/up --endpoint=https://example.com/api/v1/receive --period=10s --name foo --labels 'bar="baz"'
```
