package naming

import (
	"github.com/combust-labs/firebuild/pkg/utils"
)

// GetRandomVethName returns a random veth interface name.
func GetRandomVethName() string {
	return "veth" + utils.RandStringBytes(11)
}
