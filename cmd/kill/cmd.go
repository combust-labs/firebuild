package kill

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/combust-labs/firebuild/configs"
	"github.com/combust-labs/firebuild/pkg/utils"
	"github.com/combust-labs/firebuild/pkg/vmm"
	"github.com/combust-labs/firebuild/pkg/vmm/chroot"
	"github.com/combust-labs/firebuild/pkg/vmm/cni"
	"github.com/firecracker-microvm/firecracker-go-sdk"
	"github.com/firecracker-microvm/firecracker-go-sdk/client/models"
	"github.com/spf13/cobra"
)

// Command is the build command declaration.
var Command = &cobra.Command{
	Use:   "kill",
	Short: "Kills a running VMM",
	Run:   run,
	Long:  ``,
}

var (
	commandConfig = configs.NewKillCommandConfig()
	logConfig     = configs.NewLogginConfig()
	runCache      = configs.NewRunCacheConfig()
)

func initFlags() {
	Command.Flags().AddFlagSet(commandConfig.FlagSet())
	Command.Flags().AddFlagSet(logConfig.FlagSet())
	Command.Flags().AddFlagSet(runCache.FlagSet())
}

func init() {
	initFlags()
}

func run(cobraCommand *cobra.Command, _ []string) {

	cleanup := utils.NewDefers()
	defer cleanup.CallAll()

	rootLogger := logConfig.NewLogger("kill")

	validatingConfigs := []configs.ValidatingConfig{
		runCache,
	}

	for _, validatingConfig := range validatingConfigs {
		if err := validatingConfig.Validate(); err != nil {
			rootLogger.Error("configuration is invalid", "reason", err)
			os.Exit(1)
		}
	}

	vmmMetadata, hasMetadata, metadataErr := vmm.FetchMetadataIfExists(filepath.Join(runCache.RunCache, commandConfig.VMMID))
	if metadataErr != nil {
		rootLogger.Error("failed loading metadata", "reason", metadataErr, "vmm-id", commandConfig.VMMID, "run-cache", runCache.RunCache)
		os.Exit(1)
	}
	if !hasMetadata {
		rootLogger.Error("run cache directory did not contain the VMM metadata", "vmm-id", commandConfig.VMMID, "run-cache", runCache.RunCache)
		os.Exit(1)
	}

	chrootInst := chroot.NewWithLocation(chroot.LocationFromComponents(vmmMetadata.Configs.Jailer.ChrootBase,
		vmmMetadata.Configs.Jailer.BinaryFirecracker,
		vmmMetadata.VMMID))
	chrootExists, chrootErr := chrootInst.Exists()
	if chrootErr != nil {
		rootLogger.Error("error while checking VMM chroot", "reason", chrootErr)
		os.Exit(1)
	}
	if !chrootExists {
		rootLogger.Error("VMM not found, nothing to do")
		os.Exit(0)
	}

	if err := chrootInst.IsValid(); err != nil {
		rootLogger.Error("error while inspecting chroot", "reason", err, "chroot", chrootInst.FullPath())
	}

	// Do I have the socket file?
	socketPath, hasSocket, existsErr := chrootInst.SocketPathIfExists()
	if existsErr != nil {
		rootLogger.Error("failed checking if the VMM socket file exists", "reason", existsErr)
		os.Exit(1)
	}

	if hasSocket {
		rootLogger.Info("stopping VMM")
		fcClient := firecracker.NewClient(socketPath, nil, false)

		ok, actionErr := fcClient.CreateSyncAction(context.Background(), &models.InstanceActionInfo{
			ActionType: firecracker.String("SendCtrlAltDel"),
		})
		if actionErr != nil {
			if !strings.Contains(actionErr.Error(), "connect: connection refused") {
				rootLogger.Error("failed sending CtrlAltDel to the VMM", "reason", actionErr)
				os.Exit(1)
			}
			rootLogger.Info("VMM is already stopped")
		} else {

			rootLogger.Info("VMM with pid, waiting for process to exit")

			waitCtx, cancelFunc := context.WithTimeout(context.Background(), commandConfig.ShutdownTimeout)
			defer cancelFunc()
			chanErr := make(chan error, 1)

			go func() {
				chanErr <- vmmMetadata.PID.Wait(waitCtx)
			}()

			select {
			case <-waitCtx.Done():
				rootLogger.Error("VMM shutdown wait timed out, unclean shutdown", "reason", waitCtx.Err())
			case err := <-chanErr:
				if err != nil {
					rootLogger.Error("VMM process exit with an error", "reason", err)
				} else {
					rootLogger.Info("VMM process exit clean")
				}
			}

			rootLogger.Info("VMM stopped with response", "response", ok)
		}
	}

	rootLogger.Info("cleaning up CNI")
	if err := cni.CleanupCNI(rootLogger,
		vmmMetadata.Configs.CNI,
		commandConfig.VMMID, vmmMetadata.CNI.VethIfaceName,
		vmmMetadata.CNI.NetName, vmmMetadata.CNI.NetNS); err != nil {
		rootLogger.Error("failed cleaning up CNI", "reason", err)
		os.Exit(1)
	}
	rootLogger.Info("CNI cleaned up")

	// have to clean up the cache
	rootLogger.Info("removing the cache directory")
	cacheDirectory := filepath.Join(runCache.RunCache, commandConfig.VMMID)
	if err := os.RemoveAll(cacheDirectory); err != nil {
		rootLogger.Error("failed removing cache directroy", "reason", err, "path", cacheDirectory)
	}
	rootLogger.Info("cache directory removed")

	// have to clean up the jailer
	rootLogger.Info("removing the jailer directory")
	if err := chrootInst.RemoveAll(); err != nil {
		rootLogger.Error("failed removing jailer directroy", "reason", err, "path", chrootInst.FullPath())
	}
	rootLogger.Info("jailer directory removed")
}
