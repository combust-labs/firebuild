package configs

import "github.com/spf13/pflag"

// EgressTestConfig provides options for VMM agress test.
type EgressTestConfig struct {
	flagBase

	EgressNoWait             bool
	EgressTestTarget         string
	EgressTestTimeoutSeconds int
}

// NewEgressTestConfig returns a new instance of the configuration.
func NewEgressTestConfig() *EgressTestConfig {
	return &EgressTestConfig{}
}

// FlagSet returns an instance of the flag set for the configuration.
func (c *EgressTestConfig) FlagSet() *pflag.FlagSet {
	if c.initFlagSet() {
		c.flagSet.BoolVar(&c.EgressNoWait, "egress-no-wait", false, "When set, the build process will not wait for the VMM to have egress confirmed")
		c.flagSet.StringVar(&c.EgressTestTarget, "egress-test-target", "1.1.1.1", "Address to use for VMM egress connectivity test; IP address or FQDN, egress is tested with the ping command")
		c.flagSet.IntVar(&c.EgressTestTimeoutSeconds, "egress-test-timeout-seconds", 15, "Maxmim amount of time to wait for egress connectivity before failing the build")
	}
	return c.flagSet
}
