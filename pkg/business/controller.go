package business

import (
	"context"
	"fmt"
	"strings"

	"github.com/slamdev/external-dns-pfsense-webhook/api/externaldnsapi"
	"github.com/slamdev/external-dns-pfsense-webhook/pkg/business/svc"
	"github.com/slamdev/external-dns-pfsense-webhook/pkg/integration"
)

const descriptionPropertyName = "description"
const aliasesPropertyName = "aliases"

type controller struct {
	pfsenseService svc.PfsenseService
}

func NewController(pfsenseService svc.PfsenseService) externaldnsapi.StrictServerInterface {
	return &controller{
		pfsenseService: pfsenseService,
	}
}

func (c *controller) Negotiate(_ context.Context, _ externaldnsapi.NegotiateRequestObject) (externaldnsapi.NegotiateResponseObject, error) {
	return externaldnsapi.Negotiate200ApplicationExternalDNSWebhookPlusJSONVersion1Response{
		Filters: []string{},
	}, nil
}

func (c *controller) GetRecords(ctx context.Context, request externaldnsapi.GetRecordsRequestObject) (externaldnsapi.GetRecordsResponseObject, error) {
	hosts, err := c.pfsenseService.ListHosts(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list unbound hosts; %w", err)
	}
	endpoints, err := integration.MapSliceErr(hosts, c.asEndpoint)
	if err != nil {
		return nil, integration.NewValidationError(err.Error())
	}
	return externaldnsapi.GetRecords200ApplicationExternalDNSWebhookPlusJSONVersion1Response(endpoints), nil
}

func (c *controller) SetRecords(ctx context.Context, request externaldnsapi.SetRecordsRequestObject) (externaldnsapi.SetRecordsResponseObject, error) {
	var hostsToCreate, hostsToUpdate, hostsToDelete []svc.UnboundHost
	var err error

	if request.Body.Create != nil {
		hostsToCreate, err = integration.MapSliceErr(*request.Body.Create, c.asUnboundHost)
		if err != nil {
			return nil, integration.NewValidationError(err.Error())
		}
	}

	if request.Body.UpdateNew != nil {
		hostsToUpdate, err = integration.MapSliceErr(*request.Body.UpdateNew, c.asUnboundHost)
		if err != nil {
			return nil, integration.NewValidationError(err.Error())
		}
	}

	if request.Body.Delete != nil {
		hostsToDelete, err = integration.MapSliceErr(*request.Body.Delete, c.asUnboundHost)
		if err != nil {
			return nil, integration.NewValidationError(err.Error())
		}
	}

	if err := c.pfsenseService.ApplyHostsChanges(ctx, hostsToCreate, hostsToUpdate, hostsToDelete); err != nil {
		return nil, fmt.Errorf("failed to apply unbound hosts changes; %w", err)
	}
	return externaldnsapi.SetRecords204Response{}, nil
}

func (c *controller) AdjustRecords(_ context.Context, request externaldnsapi.AdjustRecordsRequestObject) (externaldnsapi.AdjustRecordsResponseObject, error) {
	return externaldnsapi.AdjustRecords200ApplicationExternalDNSWebhookPlusJSONVersion1Response(*request.Body), nil
}

func (c *controller) asEndpoint(host svc.UnboundHost) (externaldnsapi.Endpoint, error) {
	dnsName, err := c.buildDNSName(host.Host, host.Domain)
	if err != nil {
		return externaldnsapi.Endpoint{}, fmt.Errorf("failed to build dns name from host %+v; %w", host, err)
	}
	var props []externaldnsapi.ProviderSpecificProperty
	if host.Descr != "" {
		props = append(props, externaldnsapi.ProviderSpecificProperty{
			Name:  integration.ToPointer(descriptionPropertyName),
			Value: integration.ToPointer(host.Descr),
		})
	}
	if host.Aliases != "" {
		props = append(props, externaldnsapi.ProviderSpecificProperty{
			Name:  integration.ToPointer(aliasesPropertyName),
			Value: integration.ToPointer(host.Aliases),
		})
	}
	return externaldnsapi.Endpoint{
		DnsName:          &dnsName,
		Targets:          &[]string{host.Ip},
		ProviderSpecific: props,
		RecordType:       integration.ToPointer("A"),
	}, nil
}

func (c *controller) buildDNSName(host, domain string) (string, error) {
	if strings.Count(host, ".") != 0 {
		return "", fmt.Errorf("host can have only one part, got %+v", strings.Split(host, "."))
	}
	var name string
	if host != "" {
		name = strings.Join([]string{host, domain}, ".")
	} else {
		name = domain
	}
	return name, nil
}

func (c *controller) asUnboundHost(endpoint externaldnsapi.Endpoint) (svc.UnboundHost, error) {
	host, domain, err := explodeHostName(*endpoint.DnsName)
	if err != nil {
		return svc.UnboundHost{}, fmt.Errorf("failed to explode dns name %+v; %w", *endpoint.DnsName, err)
	}
	if endpoint.Targets == nil || len(*endpoint.Targets) != 1 {
		return svc.UnboundHost{}, fmt.Errorf("only one target is supported, got %+v; dns name: %s", endpoint.Targets, *endpoint.DnsName)
	}
	var descr, aliases string
	for _, prop := range endpoint.ProviderSpecific {
		if prop.Name == nil || prop.Value == nil {
			continue
		}
		switch *prop.Name {
		case descriptionPropertyName:
			descr = *prop.Value
		case aliasesPropertyName:
			aliases = *prop.Value
		default:
			return svc.UnboundHost{}, fmt.Errorf("unsupported provider specific property %+v; dns name: %s", *prop.Name, *endpoint.DnsName)
		}
	}
	return svc.UnboundHost{
		Host:    host,
		Domain:  domain,
		Ip:      (*endpoint.Targets)[0],
		Descr:   descr,
		Aliases: aliases,
	}, nil
}

func explodeHostName(hostName string) (string, string, error) {
	if strings.Count(hostName, ".") == 1 {
		return "", hostName, nil
	}
	parts := strings.SplitN(hostName, ".", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("host name should be in form of [<sub> <domain>], got %+v", parts)
	}
	return parts[0], parts[1], nil
}
