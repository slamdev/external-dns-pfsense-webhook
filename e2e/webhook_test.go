package e2e

import (
	"context"
	"net/http"
	"testing"

	"github.com/slamdev/external-dns-pfsense-webhook/api/externaldnsapi"
	"github.com/slamdev/external-dns-pfsense-webhook/pkg/integration"
	"github.com/slamdev/external-dns-pfsense-webhook/testdata"
	"github.com/stretchr/testify/require"
)

func Test_should_verify_webhook_endpoints(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	apiClient := testdata.CreateWebhookClient(t)

	negotiateResp, err := apiClient.NegotiateWithResponse(ctx)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, negotiateResp.StatusCode(), string(negotiateResp.Body))
	negotiate := negotiateResp.ApplicationexternalDnsWebhookJSONVersion1200
	require.Empty(t, negotiate.Filters)

	recordsResp, err := apiClient.GetRecordsWithResponse(ctx)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, recordsResp.StatusCode(), string(recordsResp.Body))
	records := recordsResp.ApplicationexternalDnsWebhookJSONVersion1200
	require.NotEmpty(t, records)

	expectedAdjustedRecords := []externaldnsapi.Endpoint{testdata.RndEndpoint(), testdata.RndEndpoint()}
	adjustResp, err := apiClient.AdjustRecordsWithApplicationExternalDNSWebhookPlusJSONVersion1BodyWithResponse(ctx, expectedAdjustedRecords)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, adjustResp.StatusCode(), string(adjustResp.Body))
	adjustedRecords := *adjustResp.ApplicationexternalDnsWebhookJSONVersion1200
	require.Equal(t, expectedAdjustedRecords, adjustedRecords)
}

// Test_should_apply_changes drives one record through create, update and delete, checking after each
// step that the change actually reached the provider. Asserting only on the 204 of applychanges is
// not enough: the provider can accept a change, report success and silently not perform it.
func Test_should_apply_changes(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	apiClient := testdata.CreateWebhookClient(t)

	record := testdata.RndEndpoint()
	dnsName := *record.DnsName

	// leave nothing behind if an assertion below fails before the delete step
	t.Cleanup(func() {
		//nolint:usetesting // t.Context() is already cancelled by the time cleanup runs
		applyChanges(context.Background(), t, apiClient, externaldnsapi.Changes{
			Delete: &[]externaldnsapi.Endpoint{record},
		})
	})

	_, found := findRecord(ctx, t, apiClient, dnsName)
	require.False(t, found, "record %s must not exist before it is created", dnsName)

	applyChanges(ctx, t, apiClient, externaldnsapi.Changes{
		Create: &[]externaldnsapi.Endpoint{record},
	})
	created, found := findRecord(ctx, t, apiClient, dnsName)
	require.True(t, found, "record %s must exist after it is created", dnsName)
	require.Equal(t, *record.Targets, *created.Targets)

	updated := testdata.CopyStruct(record)
	updated.Targets = integration.ToPointer([]string{"2.2.2.2"})
	applyChanges(ctx, t, apiClient, externaldnsapi.Changes{
		UpdateOld: &[]externaldnsapi.Endpoint{record},
		UpdateNew: &[]externaldnsapi.Endpoint{updated},
	})
	afterUpdate, found := findRecord(ctx, t, apiClient, dnsName)
	require.True(t, found, "record %s must still exist after it is updated", dnsName)
	require.Equal(t, *updated.Targets, *afterUpdate.Targets, "update must change the target of %s", dnsName)

	applyChanges(ctx, t, apiClient, externaldnsapi.Changes{
		Delete: &[]externaldnsapi.Endpoint{updated},
	})
	_, found = findRecord(ctx, t, apiClient, dnsName)
	require.False(t, found, "record %s must be gone after it is deleted", dnsName)
}

func applyChanges(ctx context.Context, t *testing.T, apiClient *externaldnsapi.ClientWithResponses, changes externaldnsapi.Changes) {
	t.Helper()
	resp, err := apiClient.SetRecordsWithApplicationExternalDNSWebhookPlusJSONVersion1BodyWithResponse(ctx, changes)
	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent, resp.StatusCode(), string(resp.Body))
}

func findRecord(ctx context.Context, t *testing.T, apiClient *externaldnsapi.ClientWithResponses, dnsName string) (externaldnsapi.Endpoint, bool) {
	t.Helper()
	resp, err := apiClient.GetRecordsWithResponse(ctx)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode(), string(resp.Body))
	records := resp.ApplicationexternalDnsWebhookJSONVersion1200
	require.NotNil(t, records)
	for _, record := range *records {
		if record.DnsName != nil && *record.DnsName == dnsName {
			return record, true
		}
	}
	return externaldnsapi.Endpoint{}, false
}
