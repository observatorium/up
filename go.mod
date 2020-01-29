module github.com/observatorium/up

go 1.13

require (
	github.com/campoy/embedmd v1.0.0
	github.com/go-kit/kit v0.9.0
	github.com/gogo/protobuf v1.3.1
	github.com/golang/snappy v0.0.1
	github.com/oklog/run v1.0.0
	github.com/pkg/errors v0.8.1
	github.com/prometheus/client_golang v1.2.1
	github.com/prometheus/client_model v0.0.0-20190812154241-14fe0d1b01d4
	github.com/prometheus/common v0.7.0
	github.com/prometheus/prometheus v1.8.2-0.20200107122003-4708915ac6ef
	github.com/thanos-io/thanos v0.10.1
)

replace (
	github.com/Azure/go-autorest => github.com/Azure/go-autorest v12.3.0+incompatible
	k8s.io/client-go => k8s.io/client-go v0.0.0-20190620085101-78d2af792bab
)
