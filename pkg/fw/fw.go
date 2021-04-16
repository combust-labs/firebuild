package fw

import (
	"fmt"
	"time"

	"github.com/combust-labs/firebuild/pkg/flock"
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

	// FirebuildFlockDefaultFile is the default IPT flock path.
	FirebuildFlockDefaultFile = "/tmp/iptables.lock"
	// FirebuildFlockFileEnvVarName is the name of the environment variable which can be used to
	// override the default flock file path.
	FirebuildFlockFileEnvVarName = "FIREBUILD_IPT_FLOCK_FILE"

	// FirebuildFlockDefaultAcquireTimeout is the default timeout value.
	FirebuildFlockDefaultAcquireTimeout = "10s"
	// FirebuildFlockAcquireTimeoutEnvVarName is the name of the environment variable which can be used to
	// override the default flock acquire timeout.
	FirebuildFlockAcquireTimeoutEnvVarName = "FIREBUILD_IPT_FLOCK_ACQUIRE_TIMEOUT"
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

	lock               flock.Lock
	lockAcquireTimeout time.Duration
	filterChainName    string
	natChainName       string
}

// NewManager returns a publisher with configured firebuild filter chain in the filter table.
// If chain fails to initialize, returns an error.
// Locking happens in:
// - ensureFilterChain, called only when creating new manager
// - in Publish
// - in Unpublish
func NewManager(vmID, ipAddress string) (IPTManager, error) {

	acquiteTimeout, err := time.ParseDuration(utils.GetenvOrDefault(FirebuildFlockAcquireTimeoutEnvVarName, FirebuildFlockDefaultAcquireTimeout))
	if err != nil {
		return nil, err
	}

	ipt, err := iptables.New()
	if err != nil {
		return nil, err
	}

	publisher := &defaultManager{ipt: ipt,
		vmID:               vmID,
		ipAddress:          ipAddress,
		lock:               flock.New(utils.GetenvOrDefault(FirebuildFlockFileEnvVarName, FirebuildFlockDefaultFile)),
		lockAcquireTimeout: acquiteTimeout,
		filterChainName:    utils.GetenvOrDefault(FirebuildIptFilterChainNameEnvVarName, FirebuildIptDefaultFilterChainName),
		natChainName:       fmt.Sprintf("FBD-%s", vmID)}
	if err := publisher.ensureFilterChain(); err != nil {
		return nil, err
	}
	return publisher, nil
}

// Publish publishes exposed ports. Creates a nat table chain if necessary.
func (p *defaultManager) Publish(ports []ExposedPort) error {

	if err := p.lock.AcquireWithTimeout(p.lockAcquireTimeout); err != nil {
		return err
	}
	defer p.lock.Release()

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

	if err := p.lock.AcquireWithTimeout(p.lockAcquireTimeout); err != nil {
		return err
	}
	defer p.lock.Release()

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

	if err := p.lock.AcquireWithTimeout(p.lockAcquireTimeout); err != nil {
		return err
	}
	defer p.lock.Release()

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
