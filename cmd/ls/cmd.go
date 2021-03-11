package ls

import (
	"io/ioutil"
	"path/filepath"

	"github.com/combust-labs/firebuild/configs"
	"github.com/combust-labs/firebuild/pkg/utils"
	"github.com/combust-labs/firebuild/pkg/vmm/pid"
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
		vmmPid, hasPid, err := pid.FetchPIDIfExists(filepath.Join(runCache.RunCache, vmmID))
		if err != nil {
			rootLogger.Error("failed loading pid file for possible VMM", "vmm-id", vmmID, "reason", err)
			continue
		}
		if hasPid {
			running, err := vmmPid.IsRunning()
			if err != nil {
				rootLogger.Error("failed checking pid status for possible VMM", "vmm-id", vmmID, "reason", err)
				continue
			}
			rootLogger.Info("vmm", "id", vmmID, "running", running, "pid", vmmPid.Pid)
		} else {
			rootLogger.Info("vmm", "id", vmmID, "running", "???", "pid", "???")
		}
	}
}
