package sock

import (
	"path/filepath"

	"github.com/combust-labs/firebuild/configs"
	"github.com/combust-labs/firebuild/pkg/utils"
)

// FetchSocketPathIfExists fetches the VMM socket path.
// Returns the socket path, a boolean indicating if the socket exists and an error if existence check went wrong.
func FetchSocketPathIfExists(jailingFcConfig *configs.JailingFirecrackerConfig) (string, bool, error) {
	socketPath := filepath.Join(jailingFcConfig.JailerChrootDirectory(), "root/run/firecracker.socket")
	hasSocket, existsErr := utils.PathExists(socketPath)
	return socketPath, hasSocket, existsErr
}
