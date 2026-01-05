package testdata

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/justinrixx/retryhttp"
	"github.com/slamdev/external-dns-pfsense-webhook/api/externaldnsapi"
	"github.com/stretchr/testify/require"
)

func CreateWebhookClient(t *testing.T) *externaldnsapi.ClientWithResponses {
	swagger, err := externaldnsapi.GetSwagger()
	require.NoError(t, err)

	url, clientSetup := apiClientParams(t, swagger, Cfg.App.URL)

	client, err := externaldnsapi.NewClientWithResponses(
		url,
		func(client *externaldnsapi.Client) error {
			client.Client = clientSetup()
			return nil
		},
	)
	require.NoError(t, err)
	return client
}

func apiClientParams(t *testing.T, swagger *openapi3.T, hostURL string) (string, func() *http.Client) {
	basePath, err := swagger.Servers.BasePath()
	require.NoError(t, err)

	url := hostURL + basePath

	clientSetup := func() *http.Client {
		return &http.Client{
			Timeout: Cfg.HTTPClientTimeout,
			Transport: retryhttp.New(
				retryhttp.WithShouldRetryFn(func(attempt retryhttp.Attempt) bool {
					return shouldRetryAttempt(t, attempt)
				}),
				retryhttp.WithTransport(func() http.RoundTripper {
					transport := http.DefaultTransport.(*http.Transport).Clone()
					transport.DisableKeepAlives = true
					return transport
				}()),
			),
		}
	}

	return url, clientSetup
}

func shouldRetryAttempt(t *testing.T, attempt retryhttp.Attempt) bool {
	endpoint := getEndpointInfo(attempt)

	// Check for retryable errors
	if attempt.Err != nil {
		shouldRetry := isRetryableError(attempt.Err)

		if shouldRetry {
			t.Logf("RETRYING ENDPOINT: %s (Attempt #%d) - Error: %v", endpoint, attempt.Count, attempt.Err)
		}
		return shouldRetry
	}

	// Check for retryable status codes
	if attempt.Res != nil {
		shouldRetry := attempt.Res.StatusCode == http.StatusServiceUnavailable

		if shouldRetry {
			t.Logf("RETRYING ENDPOINT: %s (Attempt #%d) - Status: 503 ResourceService Unavailable", endpoint, attempt.Count)
		}
		return shouldRetry
	}

	return false
}

func getEndpointInfo(attempt retryhttp.Attempt) string {
	if attempt.Req != nil {
		return fmt.Sprintf("%s %s", attempt.Req.Method, attempt.Req.URL.Path)
	}
	return "unknown endpoint"
}

func isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "EOF") || strings.Contains(errStr, "timeout")
}
