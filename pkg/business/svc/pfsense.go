package svc

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"alexejk.io/go-xmlrpc"
	"github.com/slamdev/external-dns-pfsense-webhook/pkg/integration"
)

const unboundConfigSection string = "unbound"

type pfsenseService struct {
	client *xmlrpc.Client
	dryRun bool
}

type PfsenseService interface {
	ListEndpoints(ctx context.Context) ([]UnboundEndpoint, error)
	ApplyChanges(ctx context.Context, toCreate []UnboundEndpoint, toUpdate []UnboundEndpoint, toDelete []UnboundEndpoint) error
}

func NewPfsenseService(client *xmlrpc.Client, dryRun bool) PfsenseService {
	return &pfsenseService{
		client: client,
		dryRun: dryRun,
	}
}

func (s *pfsenseService) ListEndpoints(_ context.Context) ([]UnboundEndpoint, error) {
	section, err := s.fetchUnboundSection()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch unbound section; %w", err)
	}
	endpoints, err := integration.MapSliceErr(section.Hosts, s.hostToEndpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to map hosts to endpoints; %w", err)
	}
	return endpoints, nil
}

func (s *pfsenseService) fetchUnboundSection() (unbound, error) {
	req := &struct{ Data []string }{Data: []string{unboundConfigSection}}
	res := &integration.NestedXMLRPC[unboundStruct]{}
	if err := s.client.Call("pfsense.backup_config_section", req, res); err != nil {
		return unbound{}, fmt.Errorf("failed to call %s; %w", "backup_config_section", err)
	}
	return res.Nested.Unbound, nil
}

func (s *pfsenseService) ApplyChanges(ctx context.Context, toCreate []UnboundEndpoint, toUpdate []UnboundEndpoint, toDelete []UnboundEndpoint) error {
	if len(toCreate) == 0 && len(toUpdate) == 0 && len(toDelete) == 0 {
		return nil
	}

	section, err := s.fetchUnboundSection()
	if err != nil {
		return fmt.Errorf("failed to fetch unbound section; %w", err)
	}

	finalHosts, err := s.reconcileHosts(ctx, section.Hosts, toCreate, toUpdate, toDelete)
	if err != nil {
		return err
	}

	if slices.Equal(finalHosts, section.Hosts) {
		slog.InfoContext(ctx, "all records are already up to date")
		return nil
	}

	if s.dryRun {
		slog.InfoContext(ctx, "dry run enabled, not applying changes to pfsense",
			slog.String("create", integration.ToUnsafeJSONString(toCreate)),
			slog.String("update", integration.ToUnsafeJSONString(toUpdate)),
			slog.String("delete", integration.ToUnsafeJSONString(toDelete)),
			slog.String("final", integration.ToUnsafeJSONString(finalHosts)),
		)
		return nil
	}

	section.Hosts = finalHosts

	if err := s.saveUnboundSection(section); err != nil {
		return fmt.Errorf("failed to save unbound section; %w", err)
	}
	return nil
}

// reconcileHosts folds the requested changes into the host list currently stored in pfsense and
// returns the list that should replace it. It performs no I/O: given the same inputs it always
// returns the same output.
//
// Hosts are matched on dns name *and* record type, because pfsense keeps A and TXT records in the
// same host override list. Hosts that carry no description written by this webhook are treated as
// not ours: they are always kept as they are, never deleted and never rewritten.
func (s *pfsenseService) reconcileHosts(ctx context.Context, existing []host, toCreate, toUpdate, toDelete []UnboundEndpoint) ([]host, error) {
	deleteByKey := endpointsByKey(toDelete)
	updateByKey := endpointsByKey(toUpdate)
	createByKey := endpointsByKey(toCreate)

	// keys already represented in finalHosts, so the leftover passes below do not add them twice
	applied := make(map[string]struct{}, len(toUpdate)+len(toCreate))

	var finalHosts []host
	for _, existingHost := range existing {
		dnsName, err := s.buildDNSName(existingHost.Host, existingHost.Domain)
		if err != nil {
			// we cannot tell what this host is, so we cannot safely change it
			slog.WarnContext(ctx, "keeping host with an unexpected name", "host", existingHost.Host, "domain", existingHost.Domain, "error", err)
			finalHosts = append(finalHosts, existingHost)
			continue
		}

		storedEndpoint, ours := s.decodeHostDescr(existingHost)
		key := hostKey(dnsName, recordTypeOrDefault(storedEndpoint.RecordType))

		_, wantDelete := deleteByKey[key]
		updatedEndpoint, wantUpdate := updateByKey[key]
		_, wantCreate := createByKey[key]

		if !ours {
			if wantDelete || wantUpdate || wantCreate {
				slog.WarnContext(ctx, "refusing to change a host that is not managed by this webhook", "dnsName", dnsName)
			}
			// claim the key so a create for the same name does not add a duplicate override
			applied[key] = struct{}{}
			finalHosts = append(finalHosts, existingHost)
			continue
		}

		if wantDelete {
			// leaving it out of finalHosts is what deletes it
			continue
		}

		if wantUpdate {
			updatedHost, err := s.endpointToHost(updatedEndpoint)
			if err != nil {
				return nil, fmt.Errorf("failed to convert endpoint %+v to host; %w", updatedEndpoint, err)
			}
			// aliases are configured in pfsense, not by external-dns, so an update must not drop them
			updatedHost.Aliases = existingHost.Aliases
			finalHosts = append(finalHosts, updatedHost)
			applied[key] = struct{}{}
			continue
		}

		if wantCreate {
			// it already exists; keep the stored host and drop the create
			applied[key] = struct{}{}
		}

		finalHosts = append(finalHosts, existingHost)
	}

	// sometimes external-dns reports a new host as an update
	leftovers, err := s.hostsForUnappliedEndpoints(toUpdate, applied)
	if err != nil {
		return nil, fmt.Errorf("failed to map endpoints to hosts for update; %w", err)
	}
	finalHosts = append(finalHosts, leftovers...)

	created, err := s.hostsForUnappliedEndpoints(toCreate, applied)
	if err != nil {
		return nil, fmt.Errorf("failed to map endpoints to hosts for creation; %w", err)
	}
	finalHosts = append(finalHosts, created...)

	return finalHosts, nil
}

func (s *pfsenseService) hostsForUnappliedEndpoints(endpoints []UnboundEndpoint, applied map[string]struct{}) ([]host, error) {
	var hosts []host
	for _, endpoint := range endpoints {
		key := hostKey(endpoint.DNSName, recordTypeOrDefault(endpoint.RecordType))
		if _, ok := applied[key]; ok {
			continue
		}
		newHost, err := s.endpointToHost(endpoint)
		if err != nil {
			return nil, err
		}
		hosts = append(hosts, newHost)
		applied[key] = struct{}{}
	}
	return hosts, nil
}

func endpointsByKey(endpoints []UnboundEndpoint) map[string]UnboundEndpoint {
	byKey := make(map[string]UnboundEndpoint, len(endpoints))
	for _, endpoint := range endpoints {
		byKey[hostKey(endpoint.DNSName, recordTypeOrDefault(endpoint.RecordType))] = endpoint
	}
	return byKey
}

// hostKey identifies a record: pfsense stores A and TXT records in the same list, so the dns name
// alone is not enough to tell them apart.
func hostKey(dnsName, recordType string) string {
	return dnsName + "|" + recordType
}

func recordTypeOrDefault(recordType string) string {
	if recordType == "" {
		return "A"
	}
	return recordType
}

func (s *pfsenseService) saveUnboundSection(section unbound) error {
	req := &struct {
		Sections any
		Timeout  int
	}{
		Sections: map[string]any{unboundConfigSection: section},
		Timeout:  30,
	}
	res := &integration.OperationResult{}
	if err := s.client.Call("pfsense.restore_config_section", req, res); err != nil {
		return fmt.Errorf("failed to call %s; %w", "restore_config_section", err)
	}
	if !res.Success {
		return errors.New("pfsense return 'false' as a result of config restoring")
	}
	if err := s.execPhp("$toreturn = services_unbound_configure(false);"); err != nil {
		return errors.New("failed to exec php to configure unbound")
	}
	if err := s.execPhp("$toreturn = services_dhcpd_configure();"); err != nil {
		return errors.New("failed to exec php to configure dhcpd")
	}
	return nil
}

func (s *pfsenseService) execPhp(code string) error {
	req := &struct{ Data string }{Data: code}
	res := &integration.OperationResult{}
	if err := s.client.Call("pfsense.exec_php", req, res); err != nil {
		return fmt.Errorf("failed to exec php; %w", err)
	}
	if !res.Success {
		return errors.New("pfsense return 'false' as a result of exec php")
	}
	return nil
}

func (s *pfsenseService) endpointToHost(endpoint UnboundEndpoint) (host, error) {
	if !slices.Contains([]string{"A", "TXT"}, endpoint.RecordType) {
		return host{}, fmt.Errorf("only A and TXT record types are supported, got %+v", endpoint.RecordType)
	}

	hostname, domain, err := s.explodeHostName(endpoint.DNSName)
	if err != nil {
		return host{}, fmt.Errorf("failed to explode dns name %+v; %w", endpoint.DNSName, err)
	}

	if endpoint.RecordType == "A" && len(endpoint.Targets) != 1 {
		return host{}, fmt.Errorf("only one target is supported for A record, got %+v; dns name: %s", endpoint.Targets, endpoint.DNSName)
	}

	ip := "127.0.0.1" // fake IP for non-A records
	if endpoint.RecordType == "A" {
		ip = endpoint.Targets[0]
	}

	description, _ := json.Marshal(endpoint)

	return host{
		Host:   hostname,
		Domain: domain,
		Ip:     ip,
		Descr:  base64.StdEncoding.EncodeToString(description),
	}, nil
}

func (s *pfsenseService) hostToEndpoint(host host) (UnboundEndpoint, error) {
	dnsName, err := s.buildDNSName(host.Host, host.Domain)
	if err != nil {
		return UnboundEndpoint{}, fmt.Errorf("failed to build dns name from host %+v; %w", host, err)
	}

	storedEndpoint, ours := s.decodeHostDescr(host)
	if !ours {
		// a host override configured in pfsense itself; all we know is the name it resolves to
		return UnboundEndpoint{
			DNSName:    dnsName,
			Targets:    []string{host.Ip},
			RecordType: "A",
		}, nil
	}

	return UnboundEndpoint{
		DNSName:          dnsName,
		Targets:          storedEndpoint.Targets,
		RecordType:       recordTypeOrDefault(storedEndpoint.RecordType),
		Labels:           storedEndpoint.Labels,
		ProviderSpecific: storedEndpoint.ProviderSpecific,
	}, nil
}

// decodeHostDescr reads back the endpoint this webhook stored in the host description when it wrote
// the host. The second return value reports whether the host is managed by this webhook: a host with
// no description, or one whose description we cannot read, belongs to whoever configured it in
// pfsense and must be left alone.
func (s *pfsenseService) decodeHostDescr(host host) (UnboundEndpoint, bool) {
	if host.Descr == "" {
		return UnboundEndpoint{}, false
	}
	decoded, err := base64.StdEncoding.DecodeString(host.Descr)
	if err != nil {
		slog.Warn("failed to decode base64 description", "descr", host.Descr, "host", host.Host, "domain", host.Domain, "error", err)
		return UnboundEndpoint{}, false
	}
	var endpoint UnboundEndpoint
	if err := json.Unmarshal(decoded, &endpoint); err != nil {
		slog.Warn("failed to unmarshal description to endpoint", "descr", host.Descr, "host", host.Host, "domain", host.Domain, "error", err)
		return UnboundEndpoint{}, false
	}
	return endpoint, true
}

func (s *pfsenseService) explodeHostName(hostName string) (string, string, error) {
	if strings.Count(hostName, ".") == 1 {
		return "", hostName, nil
	}
	parts := strings.SplitN(hostName, ".", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("host name should be in form of [<sub> <domain>], got %+v", parts)
	}
	return parts[0], parts[1], nil
}

func (s *pfsenseService) buildDNSName(host, domain string) (string, error) {
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

type UnboundEndpoint struct {
	DNSName          string            `json:"dnsName"`
	Targets          []string          `json:"targets,omitempty"`
	Labels           map[string]string `json:"labels,omitempty"`
	RecordType       string            `json:"recordType"`
	ProviderSpecific map[string]string `json:"providerSpecific,omitempty"`
}

type unboundStruct struct {
	Unbound unbound `xml:"unbound"`
}

//nolint:revive,staticcheck
type unbound struct {
	Enable                    string `xml:"enable"`
	Dnssec                    string `xml:"dnssec"`
	ActiveInterface           string `xml:"active_interface"`
	OutgoingInterface         string `xml:"outgoing_interface"`
	CustomOptions             string `xml:"custom_options"`
	Hideidentity              string `xml:"hideidentity"`
	Hideversion               string `xml:"hideversion"`
	Dnssecstripped            string `xml:"dnssecstripped"`
	Hosts                     []host `xml:"hosts"`
	Acls                      []acl  `xml:"acls"`
	Port                      string `xml:"port"`
	Tlsport                   string `xml:"tlsport"`
	Sslcertref                string `xml:"sslcertref"`
	SystemDomainLocalZoneType string `xml:"system_domain_local_zone_type"`
	Msgcachesize              string `xml:"msgcachesize"`
	OutgoingNumTcp            string `xml:"outgoing_num_tcp"`
	IncomingNumTcp            string `xml:"incoming_num_tcp"`
	EdnsBufferSize            string `xml:"edns_buffer_size"`
	NumQueriesPerThread       string `xml:"num_queries_per_thread"`
	JostleTimeout             string `xml:"jostle_timeout"`
	CacheMaxTtl               string `xml:"cache_max_ttl"`
	CacheMinTtl               string `xml:"cache_min_ttl"`
	InfraKeepProbing          string `xml:"infra_keep_probing"`
	InfraHostTtl              string `xml:"infra_host_ttl"`
	InfraCacheNumhosts        string `xml:"infra_cache_numhosts"`
	UnwantedReplyThreshold    string `xml:"unwanted_reply_threshold"`
	LogVerbosity              string `xml:"log_verbosity"`
	Forwarding                string `xml:"forwarding"`
}

//nolint:revive,staticcheck
type host struct {
	Host    string `xml:"host"`
	Domain  string `xml:"domain"`
	Ip      string `xml:"ip"`
	Descr   string `xml:"descr"`
	Aliases string `xml:"aliases"`
}

//nolint:revive,staticcheck
type acl struct {
	Aclid       string   `xml:"aclid"`
	Aclname     string   `xml:"aclname"`
	Aclaction   string   `xml:"aclaction"`
	Description string   `xml:"description"`
	Row         []aclRow `xml:"row"`
}

//nolint:revive,staticcheck
type aclRow struct {
	AclNetwork  string `xml:"acl_network"`
	Mask        string `xml:"mask"`
	Description string `xml:"description"`
}
