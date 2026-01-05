package e2e

import (
	"net/http"
	"testing"

	"github.com/slamdev/external-dns-pfsense-webhook/api/externaldnsapi"
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

	changes := externaldnsapi.Changes{
		Create:    &[]externaldnsapi.Endpoint{testdata.RndEndpoint(), testdata.RndEndpoint()},
		Delete:    &[]externaldnsapi.Endpoint{testdata.RndEndpoint()},
		UpdateNew: &[]externaldnsapi.Endpoint{testdata.RndEndpoint()},
	}
	setRecordsResp, err := apiClient.SetRecordsWithApplicationExternalDNSWebhookPlusJSONVersion1BodyWithResponse(ctx, changes)
	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent, setRecordsResp.StatusCode(), string(setRecordsResp.Body))
}
