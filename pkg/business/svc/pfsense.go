package svc

import (
	"context"
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
	var finalHosts []host
	for _, existingHost := range section.Hosts {
		// do not add an existing host for final host if it is marked for deletion
		if slices.ContainsFunc(toDelete, func(endpoint UnboundEndpoint) bool {
			existingDNS, err := s.buildDNSName(existingHost.Host, existingHost.Domain)
			return err != nil && existingDNS == endpoint.DNSName
		}) {
			continue
		}

		// replace existing host with updated host if it is marked for toUpdate
		updateIndex := slices.IndexFunc(toUpdate, func(endpoint UnboundEndpoint) bool {
			existingDNS, err := s.buildDNSName(existingHost.Host, existingHost.Domain)
			return err != nil && existingDNS == endpoint.DNSName
		})
		if updateIndex != -1 {
			var err error
			existingHost, err = s.endpointToHost(toUpdate[updateIndex])
			if err != nil {
				return fmt.Errorf("failed to convert endpoint %+v to host; %w", toUpdate[updateIndex], err)
			}
		}

		finalHosts = append(finalHosts, existingHost)

		// remove entry from created hosts if it already exists
		createIndex := slices.IndexFunc(toCreate, func(endpoint UnboundEndpoint) bool {
			existingDNS, err := s.buildDNSName(existingHost.Host, existingHost.Domain)
			return err != nil && existingDNS == endpoint.DNSName
		})
		if createIndex != -1 {
			toCreate = append(toCreate[:createIndex], toCreate[createIndex+1:]...)
		}
	}

	// add remaining created hosts
	hostsToCreate, err := integration.MapSliceErr(toCreate, s.endpointToHost)
	if err != nil {
		return fmt.Errorf("failed to map endpoints to hosts for creation; %w", err)
	}

	finalHosts = append(finalHosts, hostsToCreate...)

	section.Hosts = finalHosts

	if s.dryRun {
		slog.InfoContext(ctx, "dry run enabled, not applying changes to pfsense",
			slog.String("create", integration.ToUnsafeJSONString(toCreate)),
			slog.String("update", integration.ToUnsafeJSONString(toUpdate)),
			slog.String("delete", integration.ToUnsafeJSONString(toDelete)),
			slog.String("final", integration.ToUnsafeJSONString(section.Hosts)),
		)
		return nil
	}

	if err := s.saveUnboundSection(section); err != nil {
		return fmt.Errorf("failed to save unbound section; %w", err)
	}
	return nil
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
		Descr:  string(description),
	}, nil
}

func (s *pfsenseService) hostToEndpoint(host host) (UnboundEndpoint, error) {
	dnsName, err := s.buildDNSName(host.Host, host.Domain)
	if err != nil {
		return UnboundEndpoint{}, fmt.Errorf("failed to build dns name from host %+v; %w", host, err)
	}

	recordType := "A"
	targets := []string{host.Ip}
	var labels map[string]string

	if host.Descr != "" && strings.HasPrefix(host.Descr, "{") {
		var endpoint UnboundEndpoint
		if err := json.Unmarshal([]byte(host.Descr), &endpoint); err != nil {
			return UnboundEndpoint{}, fmt.Errorf("failed to unmarshal description %+v to endpoint; %w", host.Descr, err)
		}
		if endpoint.RecordType != "" {
			recordType = endpoint.RecordType
		}
		targets = endpoint.Targets
		labels = endpoint.Labels
	}

	return UnboundEndpoint{
		DNSName:    dnsName,
		Targets:    targets,
		RecordType: recordType,
		Labels:     labels,
	}, nil
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
	DNSName    string            `json:"dnsName"`
	Targets    []string          `json:"targets,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
	RecordType string            `json:"recordType"`
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
