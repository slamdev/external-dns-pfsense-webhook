package svc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"alexejk.io/go-xmlrpc"
	"github.com/slamdev/external-dns-pfsense-webhook/pkg/integration"
)

const unboundConfigSection string = "unbound"

type pfsenseService struct {
	client *xmlrpc.Client
	dryRun bool
}

type PfsenseService interface {
	ListHosts(ctx context.Context) ([]UnboundHost, error)
	ApplyHostsChanges(ctx context.Context, toCreate []UnboundHost, toUpdate []UnboundHost, toDelete []UnboundHost) error
}

func NewPfsenseService(client *xmlrpc.Client, dryRun bool) PfsenseService {
	return &pfsenseService{
		client: client,
		dryRun: dryRun,
	}
}

func (s *pfsenseService) ListHosts(_ context.Context) ([]UnboundHost, error) {
	section, err := s.fetchUnboundSection()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch unbound section; %w", err)
	}
	return section.Hosts, nil
}

func (s *pfsenseService) fetchUnboundSection() (Unbound, error) {
	req := &struct{ Data []string }{Data: []string{unboundConfigSection}}
	res := &integration.NestedXMLRPC[UnboundStruct]{}
	if err := s.client.Call("pfsense.backup_config_section", req, res); err != nil {
		return Unbound{}, fmt.Errorf("failed to call %s; %w", "backup_config_section", err)
	}
	return res.Nested.Unbound, nil
}

func (s *pfsenseService) ApplyHostsChanges(ctx context.Context, toCreate []UnboundHost, toUpdate []UnboundHost, toDelete []UnboundHost) error {
	if len(toCreate) == 0 && len(toUpdate) == 0 && len(toDelete) == 0 {
		return nil
	}

	section, err := s.fetchUnboundSection()
	if err != nil {
		return fmt.Errorf("failed to fetch unbound section; %w", err)
	}
	var finalHosts []UnboundHost
	for _, existingHost := range section.Hosts {
		// do not add an existing host for final host if it is marked for deletion
		if slices.ContainsFunc(toDelete, func(host UnboundHost) bool {
			return host.Host == existingHost.Host && host.Domain == existingHost.Domain
		}) {
			continue
		}

		// replace existing host with updated host if it is marked for toUpdate
		updateIndex := slices.IndexFunc(toUpdate, func(host UnboundHost) bool {
			return host.Host == existingHost.Host && host.Domain == existingHost.Domain
		})
		if updateIndex != -1 {
			existingHost = toUpdate[updateIndex]
		}

		finalHosts = append(finalHosts, existingHost)

		// remove entry from created hosts if it already exists
		createIndex := slices.IndexFunc(toCreate, func(host UnboundHost) bool {
			return host.Host == existingHost.Host && host.Domain == existingHost.Domain
		})
		if createIndex != -1 {
			toCreate = append(toCreate[:createIndex], toCreate[createIndex+1:]...)
		}
	}
	// add remaining created hosts
	finalHosts = append(finalHosts, toCreate...)

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

func (s *pfsenseService) saveUnboundSection(section Unbound) error {
	req := &integration.NestedXMLRPC[UnboundStruct]{Nested: UnboundStruct{Unbound: section}}
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

type UnboundStruct struct {
	Unbound Unbound `xml:"unbound"`
}

//nolint:revive,staticcheck
type Unbound struct {
	Enable                    string        `xml:"enable"`
	Dnssec                    string        `xml:"dnssec"`
	ActiveInterface           string        `xml:"active_interface"`
	OutgoingInterface         string        `xml:"outgoing_interface"`
	CustomOptions             string        `xml:"custom_options"`
	Hideidentity              string        `xml:"hideidentity"`
	Hideversion               string        `xml:"hideversion"`
	Dnssecstripped            string        `xml:"dnssecstripped"`
	Hosts                     []UnboundHost `xml:"hosts"`
	Acls                      []UnboundAcl  `xml:"acls"`
	Port                      string        `xml:"port"`
	Tlsport                   string        `xml:"tlsport"`
	Sslcertref                string        `xml:"sslcertref"`
	SystemDomainLocalZoneType string        `xml:"system_domain_local_zone_type"`
	Msgcachesize              string        `xml:"msgcachesize"`
	OutgoingNumTcp            string        `xml:"outgoing_num_tcp"`
	IncomingNumTcp            string        `xml:"incoming_num_tcp"`
	EdnsBufferSize            string        `xml:"edns_buffer_size"`
	NumQueriesPerThread       string        `xml:"num_queries_per_thread"`
	JostleTimeout             string        `xml:"jostle_timeout"`
	CacheMaxTtl               string        `xml:"cache_max_ttl"`
	CacheMinTtl               string        `xml:"cache_min_ttl"`
	InfraKeepProbing          string        `xml:"infra_keep_probing"`
	InfraHostTtl              string        `xml:"infra_host_ttl"`
	InfraCacheNumhosts        string        `xml:"infra_cache_numhosts"`
	UnwantedReplyThreshold    string        `xml:"unwanted_reply_threshold"`
	LogVerbosity              string        `xml:"log_verbosity"`
	Forwarding                string        `xml:"forwarding"`
}

//nolint:revive,staticcheck
type UnboundHost struct {
	Host    string `xml:"host"`
	Domain  string `xml:"domain"`
	Ip      string `xml:"ip"`
	Descr   string `xml:"descr"`
	Aliases string `xml:"aliases"`
}

//nolint:revive,staticcheck
type UnboundAcl struct {
	Aclid       string          `xml:"aclid"`
	Aclname     string          `xml:"aclname"`
	Aclaction   string          `xml:"aclaction"`
	Description string          `xml:"description"`
	Row         []UnboundAclRow `xml:"row"`
}

//nolint:revive,staticcheck
type UnboundAclRow struct {
	AclNetwork  string `xml:"acl_network"`
	Mask        string `xml:"mask"`
	Description string `xml:"description"`
}
