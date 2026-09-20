package svc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// reconcileHosts decides which host overrides pfsense should end up with. Every case below is
// expressed in terms of that decision: a host present in the result is kept, a host absent from it
// is deleted.
func Test_reconcileHosts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		existing []host
		toCreate []UnboundEndpoint
		toUpdate []UnboundEndpoint
		toDelete []UnboundEndpoint
		want     []host
	}{
		{
			name:     "delete removes the matching host",
			existing: []host{managedHost(t, aRecord("zot.adnz.co", "10.3.100.0")), managedHost(t, aRecord("keep.adnz.co", "10.3.100.1"))},
			toDelete: []UnboundEndpoint{aRecord("zot.adnz.co", "10.3.100.0")},
			want:     []host{managedHost(t, aRecord("keep.adnz.co", "10.3.100.1"))},
		},
		{
			name:     "delete removes the matching host when it is an apex record",
			existing: []host{managedHost(t, aRecord("adnz.co", "10.3.100.1"))},
			toDelete: []UnboundEndpoint{aRecord("adnz.co", "10.3.100.1")},
			want:     nil,
		},
		{
			name:     "delete removes the matching txt record",
			existing: []host{managedHost(t, txtRecord("a-prefix-zot.adnz.co", "heritage=external-dns"))},
			toDelete: []UnboundEndpoint{txtRecord("a-prefix-zot.adnz.co", "heritage=external-dns")},
			want:     nil,
		},
		{
			name:     "delete of a txt record leaves the a record of the same name alone",
			existing: []host{managedHost(t, aRecord("dual.adnz.co", "10.3.100.1")), managedHost(t, txtRecord("dual.adnz.co", "heritage=external-dns"))},
			toDelete: []UnboundEndpoint{txtRecord("dual.adnz.co", "heritage=external-dns")},
			want:     []host{managedHost(t, aRecord("dual.adnz.co", "10.3.100.1"))},
		},
		{
			name:     "a host this webhook did not write is never deleted",
			existing: []host{unmanagedHost()},
			toDelete: []UnboundEndpoint{aRecord("k8s.adnz.co", unmanagedIP)},
			want:     []host{unmanagedHost()},
		},
		{
			name:     "a host whose description cannot be read is never deleted",
			existing: []host{undecodableHost()},
			toDelete: []UnboundEndpoint{aRecord("manual.adnz.co", "10.4.103.3")},
			want:     []host{undecodableHost()},
		},
		{
			name:     "a host this webhook did not write is never rewritten",
			existing: []host{unmanagedHost()},
			toUpdate: []UnboundEndpoint{aRecord("k8s.adnz.co", "10.3.100.99")},
			want:     []host{unmanagedHost()},
		},
		{
			name:     "a create for a host this webhook did not write does not add a duplicate",
			existing: []host{unmanagedHost()},
			toCreate: []UnboundEndpoint{aRecord("k8s.adnz.co", "10.3.100.99")},
			want:     []host{unmanagedHost()},
		},
		{
			name:     "update replaces the host in place",
			existing: []host{managedHost(t, aRecord("moved.adnz.co", "10.3.100.1")), managedHost(t, aRecord("keep.adnz.co", "10.3.100.2"))},
			toUpdate: []UnboundEndpoint{aRecord("moved.adnz.co", "10.3.100.20")},
			want:     []host{managedHost(t, aRecord("moved.adnz.co", "10.3.100.20")), managedHost(t, aRecord("keep.adnz.co", "10.3.100.2"))},
		},
		{
			name:     "an update for a host that does not exist yet is added",
			existing: []host{managedHost(t, aRecord("keep.adnz.co", "10.3.100.1"))},
			toUpdate: []UnboundEndpoint{aRecord("new.adnz.co", "10.3.100.2")},
			want:     []host{managedHost(t, aRecord("keep.adnz.co", "10.3.100.1")), managedHost(t, aRecord("new.adnz.co", "10.3.100.2"))},
		},
		{
			name:     "create adds a host",
			existing: []host{managedHost(t, aRecord("keep.adnz.co", "10.3.100.1"))},
			toCreate: []UnboundEndpoint{aRecord("new.adnz.co", "10.3.100.2")},
			want:     []host{managedHost(t, aRecord("keep.adnz.co", "10.3.100.1")), managedHost(t, aRecord("new.adnz.co", "10.3.100.2"))},
		},
		{
			name:     "create of a host that already exists keeps the stored one",
			existing: []host{managedHost(t, aRecord("existing.adnz.co", "10.3.100.1"))},
			toCreate: []UnboundEndpoint{aRecord("existing.adnz.co", "10.3.100.20")},
			want:     []host{managedHost(t, aRecord("existing.adnz.co", "10.3.100.1"))},
		},
		{
			name: "deletes, updates and creates in one batch",
			existing: []host{
				managedHost(t, aRecord("gone.adnz.co", "10.3.100.1")),
				managedHost(t, txtRecord("a-prefix-gone.adnz.co", "heritage=external-dns")),
				managedHost(t, aRecord("moved.adnz.co", "10.3.100.1")),
				unmanagedHost(),
			},
			toCreate: []UnboundEndpoint{aRecord("fresh.adnz.co", "10.3.100.3")},
			toUpdate: []UnboundEndpoint{aRecord("moved.adnz.co", "10.3.100.20")},
			toDelete: []UnboundEndpoint{aRecord("gone.adnz.co", "10.3.100.1"), txtRecord("a-prefix-gone.adnz.co", "heritage=external-dns")},
			want: []host{
				managedHost(t, aRecord("moved.adnz.co", "10.3.100.20")),
				unmanagedHost(),
				managedHost(t, aRecord("fresh.adnz.co", "10.3.100.3")),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			service := &pfsenseService{}
			got, err := service.reconcileHosts(context.Background(), test.existing, test.toCreate, test.toUpdate, test.toDelete)
			require.NoError(t, err)
			require.Equal(t, test.want, got)
		})
	}
}

func Test_reconcileHosts_rejects_unsupported_record_types(t *testing.T) {
	t.Parallel()

	service := &pfsenseService{}
	_, err := service.reconcileHosts(context.Background(), nil, []UnboundEndpoint{{
		DNSName:    "cname.adnz.co",
		RecordType: "CNAME",
		Targets:    []string{"other.adnz.co"},
	}}, nil, nil)
	require.ErrorContains(t, err, "only A and TXT record types are supported")
}

// unmanagedIP is the address of the one host override on the firewall that this webhook did not
// write, i.e. one a human configured in pfsense.
const unmanagedIP = "10.4.103.2"

// unmanagedHost is a host override configured in pfsense itself: it carries no description, so the
// webhook cannot claim it.
func unmanagedHost() host {
	return host{Host: "k8s", Domain: "adnz.co", Ip: unmanagedIP}
}

// undecodableHost carries a description this webhook cannot read back, so it cannot claim it either.
func undecodableHost() host {
	return host{Host: "manual", Domain: "adnz.co", Ip: "10.4.103.3", Descr: "not base64 at all"}
}

func aRecord(dnsName, target string) UnboundEndpoint {
	return UnboundEndpoint{
		DNSName:    dnsName,
		RecordType: "A",
		Targets:    []string{target},
		Labels:     map[string]string{"owner": "default"},
	}
}

func txtRecord(dnsName, target string) UnboundEndpoint {
	return UnboundEndpoint{
		DNSName:    dnsName,
		RecordType: "TXT",
		Targets:    []string{target},
		Labels:     map[string]string{"ownedRecord": dnsName},
	}
}

// managedHost builds the host that pfsense stores for an endpoint, i.e. one this webhook wrote and
// therefore owns.
func managedHost(t *testing.T, endpoint UnboundEndpoint) host {
	t.Helper()
	service := &pfsenseService{}
	h, err := service.endpointToHost(endpoint)
	require.NoError(t, err)
	return h
}
