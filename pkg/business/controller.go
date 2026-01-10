package business

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/slamdev/external-dns-pfsense-webhook/api/externaldnsapi"
	"github.com/slamdev/external-dns-pfsense-webhook/pkg/business/svc"
	"github.com/slamdev/external-dns-pfsense-webhook/pkg/integration"
)

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

func (c *controller) GetRecords(ctx context.Context, _ externaldnsapi.GetRecordsRequestObject) (externaldnsapi.GetRecordsResponseObject, error) {
	unboundEndpoints, err := c.pfsenseService.ListEndpoints(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list unbound endpoints; %w", err)
	}
	externalDNSEndpoints, err := integration.MapSliceErr(unboundEndpoints, c.asExternalDNSEndpoint)
	if err != nil {
		return nil, integration.NewValidationError(err.Error())
	}
	return externaldnsapi.GetRecords200ApplicationExternalDNSWebhookPlusJSONVersion1Response(externalDNSEndpoints), nil
}

func (c *controller) SetRecords(ctx context.Context, request externaldnsapi.SetRecordsRequestObject) (externaldnsapi.SetRecordsResponseObject, error) {
	var hostsToCreate, hostsToUpdate, hostsToDelete []svc.UnboundEndpoint
	var err error

	slog.InfoContext(ctx, "external-dns wants to set records", slog.String("records", integration.ToUnsafeJSONString(request.Body)))

	if request.Body.Create != nil {
		hostsToCreate, err = integration.MapSliceErr(*request.Body.Create, c.asUnboundEndpoint)
		if err != nil {
			return nil, integration.NewValidationError(err.Error())
		}
	}

	if request.Body.UpdateNew != nil {
		hostsToUpdate, err = integration.MapSliceErr(*request.Body.UpdateNew, c.asUnboundEndpoint)
		if err != nil {
			return nil, integration.NewValidationError(err.Error())
		}
	}

	if request.Body.Delete != nil {
		hostsToDelete, err = integration.MapSliceErr(*request.Body.Delete, c.asUnboundEndpoint)
		if err != nil {
			return nil, integration.NewValidationError(err.Error())
		}
	}

	if err := c.pfsenseService.ApplyChanges(ctx, hostsToCreate, hostsToUpdate, hostsToDelete); err != nil {
		return nil, fmt.Errorf("failed to apply unbound hosts changes; %w", err)
	}
	return externaldnsapi.SetRecords204Response{}, nil
}

func (c *controller) AdjustRecords(_ context.Context, request externaldnsapi.AdjustRecordsRequestObject) (externaldnsapi.AdjustRecordsResponseObject, error) {
	return externaldnsapi.AdjustRecords200ApplicationExternalDNSWebhookPlusJSONVersion1Response(*request.Body), nil
}

func (c *controller) asExternalDNSEndpoint(endpoint svc.UnboundEndpoint) (externaldnsapi.Endpoint, error) {
	return externaldnsapi.Endpoint{
		DnsName:          &endpoint.DNSName,
		Targets:          &endpoint.Targets,
		RecordType:       &endpoint.RecordType,
		Labels:           endpoint.Labels,
		ProviderSpecific: c.fromProviderSpecificMap(endpoint.ProviderSpecific),
	}, nil
}

func (c *controller) asUnboundEndpoint(endpoint externaldnsapi.Endpoint) (svc.UnboundEndpoint, error) {
	return svc.UnboundEndpoint{
		DNSName:          integration.FromPtr(endpoint.DnsName, ""),
		Targets:          integration.FromPtr(endpoint.Targets, []string{}),
		RecordType:       integration.FromPtr(endpoint.RecordType, ""),
		Labels:           endpoint.Labels,
		ProviderSpecific: c.toProviderSpecificMap(endpoint.ProviderSpecific),
	}, nil
}

func (c *controller) toProviderSpecificMap(values []externaldnsapi.ProviderSpecificProperty) map[string]string {
	result := make(map[string]string)
	for _, v := range values {
		if v.Name != nil && *v.Name != "" && v.Value != nil && *v.Value != "" {
			result[*v.Name] = *v.Value
		}
	}
	return result
}

func (c *controller) fromProviderSpecificMap(values map[string]string) []externaldnsapi.ProviderSpecificProperty {
	result := make([]externaldnsapi.ProviderSpecificProperty, 0, len(values))
	for k, v := range values {
		key := k
		value := v
		result = append(result, externaldnsapi.ProviderSpecificProperty{
			Name:  &key,
			Value: &value,
		})
	}
	return result
}
