# UP

UP is a simple client for testing Prometheus remote-write and Loki write requests.

For a specified metric the client writes the metric at a chosen interval, where the value of the metric is always the current timestamp in milliseconds. 

It can also read the metric back from a specified endpoint and compare its value against the current time to determine the total write-read latency.

For a specified log entry the client writes the log entry at a chosen interval for the value given. 

It can also read the log back from a specified endpoint and compare the number of results.

When the given duration is greater than 0s, UP will evaluate number of errors and will exit with a non-zero code if the ratio is greater than the specified threshold.

[![Build Status](https://cloud.drone.io/api/badges/observatorium/up/status.svg)](https://cloud.drone.io/observatorium/up)

## Getting Started

The easiest way to begin making remote write requests is to run the UP container.
For example, to report an `up` metric every 10 seconds, run:

```shell
docker run --rm -p 8080:8080 quay.io/observatorium/up --endpoint-write=https://example.com/api/v1/receive --period=10s
```

Note that the metric name and labels are customizable.
For example, to report a metric named `foo` with a custom `bar` label, run:

```shell
docker run --rm -p 8080:8080 quay.io/observatorium/up --endpoint-write=https://example.com/api/v1/receive --period=10s --name foo --labels 'bar="baz"'
```

## Usage

[embedmd]:# (tmp/help.txt)
```txt
Usage of ./up:
  -duration duration
    	The duration of the up command to run until it stops. If 0 it will not stop until the process is terminated. (default 5m0s)
  -endpoint-read string
    	The endpoint to which to make query requests.
  -endpoint-type string
    	The endpoint type. Options: 'logs', 'metrics'. (default "metrics")
  -endpoint-write string
    	The endpoint to which to make remote-write requests.
  -initial-query-delay duration
    	The time to wait before executing the first query. (default 10s)
  -labels value
    	The labels in addition to '__name__' that should be applied to remote-write requests.
  -latency duration
    	The maximum allowable latency between writing and reading. (default 15s)
  -listen string
    	The address on which internal server runs. (default ":8080")
  -log.level string
    	The log filtering level. Options: 'error', 'warn', 'info', 'debug'. (default "info")
  -logs value
    	The logs that should be sent to remote-write requests.
  -logs-file string
    	A file containing logs to send against the logs write endpoint.
  -name string
    	The name of the metric to send in remote-write requests. (default "up")
  -period duration
    	The time to wait between remote-write requests. (default 5s)
  -queries-file string
    	A file containing queries to run against the read endpoint.
  -step duration
    	Default step duration for range queries. Can be overridden if step is set in query spec. (default 5m0s)
  -tenant string
    	Tenant ID to used to determine tenant for write requests.
  -tenant-header string
    	Name of HTTP header used to determine tenant for write requests. (default "tenant_id")
  -threshold float
    	The percentage of successful requests needed to succeed overall. 0 - 1. (default 0.9)
  -tls-ca-file string
    	File containing the TLS CA to use against servers for verification. If no CA is specified, there won't be any verification.
  -tls-client-cert-file string
    	File containing the default x509 Certificate for HTTPS. Leave blank to disable TLS.
  -tls-client-private-key-file string
    	File containing the default x509 private key matching --tls-cert-file. Leave blank to disable TLS.
  -token string
    	The bearer token to set in the authorization header on requests. Takes predence over --token-file if set.
  -token-file string
    	The file from which to read a bearer token to set in the authorization header on requests.
```
