package fw

import (
	"fmt"
	"os"
	"os/user"
	"testing"

	"github.com/coreos/go-iptables/iptables"
	"github.com/stretchr/testify/assert"
)

func TestIPTPublishAndCleanup(t *testing.T) {

	user, err := user.Current()

	if user.Uid != "0" {
		t.Skip("Skipping tests depending on sudo: run tests with sudo to have this test executed")
	}

	vmID := "testvm"
	targetAddress := "127.0.0.1"
	testFilterChainName := "TEST-CHAIN"
	natChainName := fmt.Sprintf("FBD-%s", vmID)

	ports := []ExposedPort{
		&defaultExposedPort{
			iface:           nil,
			hostPort:        1234,
			destinationPort: 12345,
			protocol:        "tcp",
		},
		&defaultExposedPort{
			iface:           pstring("eno1"),
			hostPort:        2345,
			destinationPort: 23456,
			protocol:        "udp",
		},
	}

	os.Setenv(FirebuildIptFilterChainNameEnvVarName, testFilterChainName)

	mgr, err := NewManager(vmID, targetAddress)
	assert.Nil(t, err)

	publishErr := mgr.Publish(ports)
	assert.Nil(t, publishErr)

	ipt, err := iptables.New()
	assert.Nil(t, err)

	fcexists, err := ipt.ChainExists("filter", testFilterChainName)
	assert.Nil(t, err)
	assert.True(t, fcexists)

	ncexists, err := ipt.ChainExists("nat", natChainName)
	assert.Nil(t, err)
	assert.True(t, ncexists)

	ruleexists, err := ipt.Exists("filter", "FORWARD", "-j", testFilterChainName)
	assert.Nil(t, err)
	assert.True(t, ruleexists)

	ruleexists, err = ipt.Exists("nat", "PREROUTING", "-j", natChainName)
	assert.Nil(t, err)
	assert.True(t, ruleexists)

	for _, port := range ports {
		ruleexists, err = ipt.Exists("filter", testFilterChainName, port.ToForwardRulespec(targetAddress)...)
		assert.Nil(t, err)
		assert.True(t, ruleexists)

		ruleexists, err = ipt.Exists("nat", natChainName, port.ToNATRulespec(targetAddress)...)
		assert.Nil(t, err)
		assert.True(t, ruleexists)
	}

	// cleanup:
	unpublishErr := mgr.Unpublish(ports)
	assert.Nil(t, unpublishErr)

	// forward chain should remain:
	fcexists, err = ipt.ChainExists("filter", testFilterChainName)
	assert.Nil(t, err)
	assert.True(t, fcexists)

	// VM chain should be removed from nat table:
	ncexists, err = ipt.ChainExists("nat", fmt.Sprintf("FBD-%s", vmID))
	assert.Nil(t, err)
	assert.False(t, ncexists)

	// the forward rule to the firebuild chain should remain:
	ruleexists, err = ipt.Exists("filter", "FORWARD", "-j", testFilterChainName)
	assert.Nil(t, err)
	assert.True(t, ruleexists)

	// nat prerouting rule must be gone:
	ruleexists, err = ipt.Exists("nat", "PREROUTING", "-j", natChainName)
	assert.NotNil(t, err)

	for _, port := range ports {
		ruleexists, err = ipt.Exists("filter", testFilterChainName, port.ToForwardRulespec(targetAddress)...)
		assert.Nil(t, err)
		assert.False(t, ruleexists)

		ruleexists, err = ipt.Exists("nat", natChainName, port.ToNATRulespec(targetAddress)...)
		assert.Nil(t, err)
		assert.False(t, ruleexists)
	}

}
