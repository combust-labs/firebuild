package fw

import (
	"fmt"

	"github.com/coreos/go-iptables/iptables"
	"github.com/pkg/errors"
)

// Maximum chain name length is 29 characters
// VM ID is maximum 20 characters + '-' leaves us with 8 characters free.

// this cannot handle concurrent requests
// this needs to run as a daemon on the host

const (
	firebuildFilterChainName    = "FIREBUILD-FILTER"
	firebuildFilterChainComment = "firebuild:forward"
)

type IPTPublisher interface {
	Publish([]string) error
}

type defaultPublisher struct {
	ipt       *iptables.IPTables
	vmID      string
	ipAddress string

	natChainName string
}

func NewPublisher(vmID, ipAddress string) (IPTPublisher, error) {
	ipt, err := iptables.New()
	if err != nil {
		return nil, err
	}
	publisher := &defaultPublisher{ipt: ipt,
		vmID:         vmID,
		ipAddress:    ipAddress,
		natChainName: fmt.Sprintf("FBD-%s", vmID)}
	if err := publisher.ensureFilterChain(); err != nil {
		return nil, err
	}
	if err := publisher.ensureNATChain(); err != nil {
		return nil, err
	}
	return publisher, nil
}

func (p *defaultPublisher) Publish(ports []string) error {
	for _, port := range ports {
		if err := p.ipt.AppendUnique("filter", firebuildFilterChainName,
			"-m", "comment", "--comment", fmt.Sprintf("firebuild:%s:%s", p.vmID, port),
			"-p", "tcp", "-d", p.ipAddress, "--dport", port,
			"-m", "state", "--state", "NEW,ESTABLISHED,RELATED", "-j", "ACCEPT"); err != nil {
			return errors.Wrapf(err, "failed exposing filter table port: %s", port)
		}
		if err := p.ipt.AppendUnique("nat", p.natChainName,
			"-m", "comment", "--comment", fmt.Sprintf("firebuild:%s:%s", p.vmID, port),
			"-p", "tcp", "--dport", port,
			"-j", "DNAT",
			"--to-destination", fmt.Sprintf("%s:%s", p.ipAddress, port)); err != nil {
			return errors.Wrapf(err, "failed exposing nat table port: %s", port)
		}
	}
	return nil
}

func (p *defaultPublisher) ensureFilterChain() error {
	if err := p.ensureChain("filter", firebuildFilterChainName); err != nil {
		return err
	}
	if err := p.ipt.AppendUnique("filter", "FORWARD",
		"-m", "comment", "--comment", firebuildFilterChainComment,
		"-j", firebuildFilterChainName); err != nil {
		return err
	}
	return nil
}

func (p *defaultPublisher) ensureNATChain() error {
	if err := p.ensureChain("nat", p.natChainName); err != nil {
		return err
	}
	if err := p.ipt.AppendUnique("nat", "PREROUTING",
		"-m", "comment", "--comment", fmt.Sprintf("firebuild:managed:%s", p.vmID),
		"-j", p.natChainName); err != nil {
		return err
	}
	return nil
}

func (p *defaultPublisher) ensureChain(table, name string) error {
	exists, err := p.ipt.ChainExists(table, name)
	if err != nil {
		return err
	}
	if !exists {
		return p.ipt.NewChain(table, name)
	}
	return nil
}
