package fw

import (
	"fmt"

	"github.com/combust-labs/firebuild/pkg/utils"
	"github.com/coreos/go-iptables/iptables"
	"github.com/pkg/errors"
)

// Maximum chain name length is 29 characters
// VM ID is maximum 20 characters + '-' leaves us with 8 characters free.

const (
	// FirebuildIptFilterChainNameEnvVarName is the name of the environment variable which can be used
	// to override the default firebuild filter chain name.
	FirebuildIptFilterChainNameEnvVarName = "FIREBUILD_IPT_FILTER_CHAIN_NAME"
	// FirebuildIptDefaultFilterChainName is the default firebuild filer chain name.
	FirebuildIptDefaultFilterChainName = "FIREBUILD-FILTER"
)

// IPTManager manages filter and nat rules for VM exposed ports.
type IPTManager interface {
	// Publish publishes exposed ports. Creates a nat table chain if necessary.
	Publish([]ExposedPort) error
	// Unpublish removes exposed ports. Removes the nat table chain if necessary.
	Unpublish([]ExposedPort) error
}

type defaultManager struct {
	ipt       *iptables.IPTables
	vmID      string
	ipAddress string

	filterChainName string
	natChainName    string
}

// NewPublisher returns a publisher with configured firebuild filter chain in the filter table.
// If chain fails to initialize, returns an error.
// Locking happens in:
// - this initializer because the top level filter chain is created here
// - in Publish
// - in Unpublish
func NewPublisher(vmID, ipAddress string) (IPTManager, error) {
	ipt, err := iptables.New()
	if err != nil {
		return nil, err
	}
	publisher := &defaultManager{ipt: ipt,
		vmID:            vmID,
		ipAddress:       ipAddress,
		filterChainName: utils.GetenvOrDefault(FirebuildIptFilterChainNameEnvVarName, FirebuildIptDefaultFilterChainName),
		natChainName:    fmt.Sprintf("FBD-%s", vmID)}
	// TODO: file system based lock
	if err := publisher.ensureFilterChain(); err != nil {
		return nil, err
	}
	return publisher, nil
}

// Publish publishes exposed ports. Creates a nat table chain if necessary.
func (p *defaultManager) Publish(ports []ExposedPort) error {
	// TODO: file system based lock
	if err := p.ensureNATChain(); err != nil {
		return err
	}
	for _, port := range ports {
		if err := p.ipt.AppendUnique("filter", p.filterChainName, port.ToForwardRulespec(p.ipAddress)...); err != nil {
			return errors.Wrapf(err, "failed exposing filter table port: %s", port)
		}
		if err := p.ipt.AppendUnique("nat", p.natChainName, port.ToNATRulespec(p.ipAddress)...); err != nil {
			return errors.Wrapf(err, "failed exposing nat table port: %s", port)
		}
	}
	return nil
}

// Unpublish removes exposed ports. Removes the nat table chain if necessary.
func (p *defaultManager) Unpublish(ports []ExposedPort) error {
	// TODO: file system based lock
	for _, port := range ports {
		if err := p.ipt.DeleteIfExists("filter", p.filterChainName, port.ToForwardRulespec(p.ipAddress)...); err != nil {
			return errors.Wrapf(err, "failed removing filter table port: %s", port)
		}
		if err := p.ipt.DeleteIfExists("nat", p.natChainName, port.ToNATRulespec(p.ipAddress)...); err != nil {
			return errors.Wrapf(err, "failed removing nat table port: %s", port)
		}
	}
	return p.removeNATChain()
}

func (p *defaultManager) ensureFilterChain() error {
	if err := ensureChain(p.ipt, "filter", p.filterChainName); err != nil {
		return err
	}
	if err := p.ipt.AppendUnique("filter", "FORWARD", "-j", p.filterChainName); err != nil {
		return err
	}
	return nil
}

func (p *defaultManager) ensureNATChain() error {
	if err := ensureChain(p.ipt, "nat", p.natChainName); err != nil {
		return err
	}
	if err := p.ipt.AppendUnique("nat", "PREROUTING", "-j", p.natChainName); err != nil {
		return err
	}
	return nil
}

func (p *defaultManager) removeNATChain() error {
	if err := p.ipt.DeleteIfExists("nat", "PREROUTING", "-j", p.natChainName); err != nil {
		return err
	}
	if err := removeChain(p.ipt, "nat", p.natChainName); err != nil {
		return err
	}
	return nil
}

func ensureChain(ipt *iptables.IPTables, table, name string) error {
	exists, err := ipt.ChainExists(table, name)
	if err != nil {
		return err
	}
	if !exists {
		return ipt.NewChain(table, name)
	}
	return nil
}

func removeChain(ipt *iptables.IPTables, table, name string) error {
	exists, err := ipt.ChainExists(table, name)
	if err != nil {
		return err
	}
	if exists {
		return ipt.DeleteChain(table, name)
	}
	return nil
}
