package purge

import (
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/combust-labs/firebuild/configs"
	"github.com/combust-labs/firebuild/pkg/utils"
	"github.com/combust-labs/firebuild/pkg/vmm"
	"github.com/combust-labs/firebuild/pkg/vmm/chroot"
	"github.com/combust-labs/firebuild/pkg/vmm/cni"
	"github.com/spf13/cobra"
)

// Command is the build command declaration.
var Command = &cobra.Command{
	Use:   "purge",
	Short: "Purges all remains of a VMM, if the VMM is stopped",
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

	rootLogger := logConfig.NewLogger("purge").With("run-cache", runCache.RunCache)

	validatingConfigs := []configs.ValidatingConfig{
		runCache,
	}

	for _, validatingConfig := range validatingConfigs {
		if err := validatingConfig.Validate(); err != nil {
			rootLogger.Error("configuration is invalid", "reason", err)
			os.Exit(1)
		}
	}

	fileInfos, readDirErr := ioutil.ReadDir(runCache.RunCache)
	if readDirErr != nil {
		rootLogger.Error("error listing run cache directory", "reason", readDirErr)
	}
	for _, fileInfo := range fileInfos {

		// see if the metadata file can be loaded:
		entry := fileInfo.Name()
		vmmMetadata, hasMetadata, err := vmm.FetchMetadataIfExists(filepath.Join(runCache.RunCache, entry))
		if err != nil {
			rootLogger.Error("metadata error for cache entry, skipping", "entry", entry, "reason", err)
			continue
		}

		if hasMetadata {

			vmmLogger := rootLogger.With("vmm-id", vmmMetadata.VMMID)

			running, err := vmmMetadata.PID.IsRunning()
			if err != nil {
				vmmLogger.Error("pid error for cache entry", "reason", err)
				continue
			}

			if running {
				vmmLogger.Debug("skipping running VMM")
				continue
			}

			// get the chroot:
			chrootInst := chroot.NewWithLocation(chroot.LocationFromComponents(vmmMetadata.Configs.Jailer.ChrootBase,
				vmmMetadata.Configs.Jailer.BinaryFirecracker,
				vmmMetadata.VMMID))
			chrootExists, chrootErr := chrootInst.Exists()
			if chrootErr != nil {
				vmmLogger.Error("error while checking VMM chroot, skipping", "reason", chrootErr)
			}

			if chrootErr == nil && chrootExists {
				if err := chrootInst.RemoveAll(); err != nil {
					vmmLogger.Error("error removing chroot directory fro stopped VMM", "reason", err)
				}
			}

			if err := cni.CleanupCNI(rootLogger,
				vmmMetadata.Configs.CNI,
				vmmMetadata.VMMID, vmmMetadata.CNI.VethIfaceName,
				vmmMetadata.CNI.NetName, vmmMetadata.CNI.NetNS); err != nil {
				vmmLogger.Error("failed cleaning up CNI", "reason", err)
			}

			// have to clean up the cache
			cacheDirectory := filepath.Join(runCache.RunCache, vmmMetadata.VMMID)
			if err := os.RemoveAll(cacheDirectory); err != nil {
				vmmLogger.Error("failed removing cache directroy", "reason", err, "path", cacheDirectory)
			}

			vmmLogger.Info(vmmMetadata.VMMID)
		} else {
			rootLogger.Warn("no metadata for entry, skipping", "entry", entry)
		}
	}
}
