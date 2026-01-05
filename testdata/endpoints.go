package testdata

import (
	"github.com/slamdev/external-dns-pfsense-webhook/api/externaldnsapi"
	"github.com/slamdev/external-dns-pfsense-webhook/pkg/integration"
)

type rndEndpointOpt func(endpoint *externaldnsapi.Endpoint)

func RndEndpoint(opts ...rndEndpointOpt) externaldnsapi.Endpoint {
	endpoint := externaldnsapi.Endpoint{
		DnsName: integration.ToPointer(RndName() + "." + RndName() + ".com"),
		Labels: map[string]string{
			"env": "test",
		},
		Targets:    integration.ToPointer([]string{"1.1.1.1"}),
		RecordType: integration.ToPointer("A"),
	}
	for _, opt := range opts {
		opt(&endpoint)
	}
	return endpoint
}
