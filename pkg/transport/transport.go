package transport

import (
	"net"
	"net/http"
	"time"

	"github.com/go-kit/log"
	"github.com/observatorium/up/pkg/options"
	"github.com/pkg/errors"
)

func NewTLSTransport(l log.Logger, tls options.TLS) (*http.Transport, error) {
	tlsConfig, err := newTLSConfig(l, tls.Cert, tls.Key, tls.CACert)
	if err != nil {
		return nil, errors.Wrap(err, "tls config")
	}

	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
			DualStack: true,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig:       tlsConfig,
	}, nil
}
