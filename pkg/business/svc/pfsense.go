package svc

import (
	"context"
	"fmt"

	"alexejk.io/go-xmlrpc"
	"github.com/slamdev/external-dns-pfsense-webhook/pkg/integration"
)

const unboundConfigSection string = "unbound"

type pfsenseService struct {
	client *xmlrpc.Client
}

type PfsenseService interface {
	ListHosts(ctx context.Context) ([]UnboundHost, error)
	ApplyHostsChanges(ctx context.Context, create []UnboundHost, update []UnboundHost, delete []UnboundHost) error
}

func NewPfsenseService(client *xmlrpc.Client) PfsenseService {
	return &pfsenseService{
		client: client,
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

func (s *pfsenseService) ApplyHostsChanges(_ context.Context, create []UnboundHost, update []UnboundHost, delete []UnboundHost) error {
	//TODO implement me
	panic("implement me")
}

type UnboundStruct struct {
	Unbound Unbound `xml:"unbound"`
}

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

type UnboundHost struct {
	Host    string `xml:"host"`
	Domain  string `xml:"domain"`
	Ip      string `xml:"ip"`
	Descr   string `xml:"descr"`
	Aliases string `xml:"aliases"`
}

type UnboundAcl struct {
	Aclid       string          `xml:"aclid"`
	Aclname     string          `xml:"aclname"`
	Aclaction   string          `xml:"aclaction"`
	Description string          `xml:"description"`
	Row         []UnboundAclRow `xml:"row"`
}

type UnboundAclRow struct {
	AclNetwork  string `xml:"acl_network"`
	Mask        string `xml:"mask"`
	Description string `xml:"description"`
}
