package ls

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	"time"

	"github.com/combust-labs/firebuild/configs"
	"github.com/combust-labs/firebuild/pkg/utils"
	"github.com/combust-labs/firebuild/pkg/vmm"
	"github.com/spf13/cobra"
)

// Command is the build command declaration.
var Command = &cobra.Command{
	Use:   "ls",
	Short: "Lists VMMs",
	Run:   run,
	Long:  ``,
}

var (
	logConfig = configs.NewLogginConfig()
	runCache  = configs.NewRunCacheConfig()
)

func initFlags() {
	Command.Flags().AddFlagSet(logConfig.FlagSet())
	Command.Flags().AddFlagSet(runCache.FlagSet())
}

func init() {
	initFlags()
}

func run(cobraCommand *cobra.Command, _ []string) {
	cleanup := utils.NewDefers()
	defer cleanup.CallAll()
	rootLogger := logConfig.NewLogger("ls")
	fileInfos, readDirErr := ioutil.ReadDir(runCache.RunCache)
	if readDirErr != nil {
		rootLogger.Error("error listing run cache directory", "reason", readDirErr)
	}
	for _, fileInfo := range fileInfos {
		// see if the pid file can be loaded:
		vmmID := fileInfo.Name()
		vmmMetadata, hasMetadata, err := vmm.FetchMetadataIfExists(filepath.Join(runCache.RunCache, vmmID))
		if err != nil {
			rootLogger.Error("failed loading metadata file for possible VMM", "vmm-id", vmmID, "reason", err)
			continue
		}
		if hasMetadata {
			running, err := vmmMetadata.PID.IsRunning()
			if err != nil {
				rootLogger.Error("failed checking pid status for possible VMM", "vmm-id", vmmID, "reason", err)
				continue
			}
			rootLogger.Info("vmm", "id", vmmID,
				"running", running,
				"pid", vmmMetadata.PID.Pid,
				"image", fmt.Sprintf("%s/%s:%s", vmmMetadata.Rootfs.Image.Org, vmmMetadata.Rootfs.Image.Image, vmmMetadata.Rootfs.Image.Version),
				"started", time.Unix(vmmMetadata.StartedAtUTC, 0).UTC().String(),
				"ip-address", vmmMetadata.NetworkInterfaces[0].StaticConfiguration.IPConfiguration.IP)
		} else {
			rootLogger.Info("vmm", "id", vmmID, "running", "???", "pid", "???")
		}
	}
}
