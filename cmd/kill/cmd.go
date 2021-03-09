package kill

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/combust-labs/firebuild/configs"
	"github.com/combust-labs/firebuild/pkg/cni"
	"github.com/combust-labs/firebuild/pkg/utils"
	"github.com/combust-labs/firebuild/pkg/vmm/pid"
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

/*
sudo /usr/local/go/bin/go run ./main.go kill \
	--binary-firecracker=$(readlink /usr/bin/firecracker) \
    --binary-jailer=$(readlink /usr/bin/jailer) \
    --chroot-base=/srv/jailer \
	--vmm-id=ad120b6306cd4c3f995870e1a8434b87
*/

var (
	cniConfig       = configs.NewCNIConfig()
	commandConfig   = configs.NewKillCommandConfig()
	jailingFcConfig = configs.NewJailingFirecrackerConfig()
	logConfig       = configs.NewLogginConfig()
	runCache        = configs.NewRunCacheConfig()
)

func initFlags() {
	Command.Flags().AddFlagSet(cniConfig.FlagSet())
	Command.Flags().AddFlagSet(commandConfig.FlagSet())
	Command.Flags().AddFlagSet(jailingFcConfig.FlagSet())
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
		commandConfig,
		jailingFcConfig,
		runCache,
	}

	for _, validatingConfig := range validatingConfigs {
		if err := validatingConfig.Validate(); err != nil {
			rootLogger.Error("configuration is invalid", "reason", err)
			os.Exit(1)
		}
	}

	jailingFcConfig.WithVMMID(commandConfig.VMMID)

	// Check if the VMM ID together with chroot is an actual jailer directory

	// 1. Check if there is anything to do:
	if _, err := utils.CheckIfExistsAndIsDirectory(jailingFcConfig.JailerChrootDirectory()); err != nil {
		if os.IsNotExist(err) {
			// nothing to do
			rootLogger.Error("VMM not found")
			os.Exit(0)
		}
	}

	// 2. See if we are really dealing with a jailer chroot:
	expectedItems := []string{
		"root/dev",
		fmt.Sprintf("root/%s", filepath.Base(jailingFcConfig.BinaryFirecracker)),
		"root/run",
	}

	items, globErr := filepath.Glob(jailingFcConfig.JailerChrootDirectory() + "/**/**")
	if globErr != nil {
		rootLogger.Error("failed validating chroot directory", "reason", globErr)
		os.Exit(1)
	}
	for idx, entry := range items {
		items[idx] = strings.TrimPrefix(entry, jailingFcConfig.JailerChrootDirectory()+"/")
	}
	for _, expected := range expectedItems {
		found := false
		for _, item := range items {
			if item == expected {
				found = true
				break
			}
		}
		if !found {
			rootLogger.Error("directory does not look like a jailer directory", "directory", jailingFcConfig.JailerChrootDirectory())
			os.Exit(1)
		}
	}

	// Do I have the socket file?
	socketPath := filepath.Join(jailingFcConfig.JailerChrootDirectory(), "root/run/firecracker.socket")
	hasSocket, existsErr := utils.PathExists(socketPath)
	if existsErr != nil {
		rootLogger.Error("failed checking if the VMM socket file exists", "reason", existsErr)
		os.Exit(1)
	}

	if hasSocket {
		rootLogger.Info("stopping VMM")
		fcClient := firecracker.NewClient(socketPath, nil, false)

		runningVMMPid, hasPid, pidErr := pid.FetchPIDIfExists(filepath.Join(runCache.RunCache, commandConfig.VMMID))
		if pidErr != nil {
			rootLogger.Error("failed fetching PID file", "reason", pidErr)
			os.Exit(1)
		}

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
			if hasPid {
				rootLogger.Info("VMM with pid, waiting for process to exit")

				waitCtx, cancelFunc := context.WithTimeout(context.Background(), time.Second*15)
				defer cancelFunc()
				chanErr := make(chan error, 1)

				go func() {
					chanErr <- runningVMMPid.Wait(waitCtx)
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

			}
			rootLogger.Info("VMM stopped with response", "response", ok)
		}
	}

	// have to clean up the CNI network:
	cniMetadataFile := filepath.Join(runCache.RunCache, commandConfig.VMMID, "cni")
	hasVethFile := true
	if _, err := utils.CheckIfExistsAndIsRegular(cniMetadataFile); err != nil {
		if !os.IsNotExist(err) {
			rootLogger.Error("failed checking if the CNI metadata file exists", "reason", err)
			os.Exit(1)
		} else {
			rootLogger.Warn("CNI metadata file not found, no automatic way to clean CNI", "reason", err)
			hasVethFile = false
		}
	}

	if hasVethFile {
		cniMetadataBytes, err := ioutil.ReadFile(cniMetadataFile)
		if err != nil {
			rootLogger.Error("failed reading the CNI meatadata file", "reason", err)
			os.Exit(1)
		}
		cniMetadata := &configs.RunningVMMCNIMetadata{}
		if err := json.Unmarshal(cniMetadataBytes, cniMetadata); err != nil {
			rootLogger.Error("failed unmarshaling the CNI metadata file", "reason", err)
			os.Exit(1)
		}
		rootLogger.Info("cleaning up CNI")
		if err := cni.CleanupCNI(rootLogger,
			cniMetadata.Config,
			commandConfig.VMMID, cniMetadata.VethIfaceName,
			cniMetadata.NetName, cniMetadata.NetNS); err != nil {
			rootLogger.Error("failed cleaning up CNI", "reason", err)
			os.Exit(1)
		}
		rootLogger.Info("CNI cleaned up")
	}

	// have to clean up the cache
	rootLogger.Info("removing the cache directory")
	cacheDirectory := filepath.Join(runCache.RunCache, commandConfig.VMMID)
	if err := os.RemoveAll(cacheDirectory); err != nil {
		rootLogger.Error("failed removing cache directroy", "reason", err, "path", cacheDirectory)
	}
	rootLogger.Info("cache directory removed")

	// have to clean up the jailer
	rootLogger.Info("removing the jailer directory")
	if err := os.RemoveAll(jailingFcConfig.JailerChrootDirectory()); err != nil {
		rootLogger.Error("failed removing jailer directroy", "reason", err, "path", jailingFcConfig.JailerChrootDirectory())
	}
	rootLogger.Info("jailer directory removed")
}
