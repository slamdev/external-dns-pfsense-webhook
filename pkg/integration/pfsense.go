package integration

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"

	"alexejk.io/go-xmlrpc"
	"github.com/alexliesenfeld/health"
)

func CreatePfsenseClient(url string, username string, password string, insecure bool) (*xmlrpc.Client, error) {
	//nolint:gosec
	httpClient := NewHTTPClientWithTLS("pfsense", &tls.Config{InsecureSkipVerify: insecure})
	headers := map[string]string{
		"Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password)),
	}
	url += "/xmlrpc.php"
	c, err := xmlrpc.NewClient(url, xmlrpc.HttpClient(httpClient), xmlrpc.Headers(headers))
	if err != nil {
		return nil, fmt.Errorf("failed to create xmlrpc client; %w", err)
	}
	return c, nil
}

func PfsenseHealthCheck(client *xmlrpc.Client) health.Check {
	return health.Check{
		Name: "pfsense",
		Check: func(_ context.Context) error {
			req := &struct {
				Dummy   string
				Timeout int
			}{
				Dummy:   "dummy_value",
				Timeout: 30,
			}
			res := &NestedXMLRPC[hostFirmwareVersionResponse]{}
			if err := client.Call("pfsense.host_firmware_version", req, res); err != nil {
				return fmt.Errorf("failed to make rpc call; %w", err)
			}
			return nil
		},
	}
}

type NestedXMLRPC[T any] struct {
	Nested T
}

type hostFirmwareVersionResponse struct {
	Firmware struct {
		Version string
	}
	Kernel struct {
		Version string
	}
	Base struct {
		Version string
	}
	Platform      string
	ConfigVersion string
}

type OperationResult struct {
	Success bool
}
