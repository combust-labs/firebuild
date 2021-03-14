package kill

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/combust-labs/firebuild/configs"
	"github.com/combust-labs/firebuild/pkg/profiles"
	"github.com/combust-labs/firebuild/pkg/tracing"
	"github.com/combust-labs/firebuild/pkg/utils"
	"github.com/combust-labs/firebuild/pkg/vmm"
	"github.com/combust-labs/firebuild/pkg/vmm/chroot"
	"github.com/combust-labs/firebuild/pkg/vmm/cni"
	"github.com/firecracker-microvm/firecracker-go-sdk"
	"github.com/firecracker-microvm/firecracker-go-sdk/client/models"
	"github.com/opentracing/opentracing-go"
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
	commandConfig  = configs.NewKillCommandConfig()
	logConfig      = configs.NewLogginConfig()
	profilesConfig = configs.NewProfileCommandConfig()
	runCache       = configs.NewRunCacheConfig()
	tracingConfig  = configs.NewTracingConfig("firebuild-vmm-kill")
)

func initFlags() {
	Command.Flags().AddFlagSet(commandConfig.FlagSet())
	Command.Flags().AddFlagSet(logConfig.FlagSet())
	Command.Flags().AddFlagSet(profilesConfig.FlagSet())
	Command.Flags().AddFlagSet(runCache.FlagSet())
	Command.Flags().AddFlagSet(tracingConfig.FlagSet())
}

func init() {
	initFlags()
}

func run(cobraCommand *cobra.Command, _ []string) {
	os.Exit(processCommand())
}

func processCommand() int {

	cleanup := utils.NewDefers()
	defer cleanup.CallAll()

	rootLogger := logConfig.NewLogger("kill")

	if profilesConfig.Profile != "" {
		profile, err := profiles.ReadProfile(profilesConfig.Profile, profilesConfig.ProfileConfDir)
		if err != nil {
			rootLogger.Error("failed resolving profile", "reason", err, "profile", profilesConfig.Profile)
			return 1
		}
		if err := profile.UpdateConfigs(runCache, tracingConfig); err != nil {
			rootLogger.Error("error updating configuration from profile", "reason", err)
			return 1
		}
	}

	tracer, tracerCleanupFunc, tracerErr := tracing.GetTracer(rootLogger.Named("tracer"), tracingConfig)
	if tracerErr != nil {
		rootLogger.Error("failed constructing tracer", "reason", tracerErr)
		return 1
	}

	cleanup.Add(tracerCleanupFunc)

	validatingConfigs := []configs.ValidatingConfig{
		runCache,
	}

	for _, validatingConfig := range validatingConfigs {
		if err := validatingConfig.Validate(); err != nil {
			rootLogger.Error("configuration is invalid", "reason", err)
			return 1
		}
	}

	spanKill := tracer.StartSpan("kill")
	spanKill.SetTag("vmm-id", commandConfig.VMMID)
	defer spanKill.Finish()

	spanFetchMetadata := tracer.StartSpan("fetch-metadata", opentracing.ChildOf(spanKill.Context()))

	vmmMetadata, hasMetadata, metadataErr := vmm.FetchMetadataIfExists(filepath.Join(runCache.RunCache, commandConfig.VMMID))
	if metadataErr != nil {
		rootLogger.Error("failed loading metadata", "reason", metadataErr, "vmm-id", commandConfig.VMMID, "run-cache", runCache.RunCache)
		spanFetchMetadata.SetBaggageItem("error", metadataErr.Error())
		spanFetchMetadata.Finish()
		return 1
	}

	spanFetchMetadata.SetTag("has-metadata", hasMetadata)

	if !hasMetadata {
		rootLogger.Error("run cache directory did not contain the VMM metadata", "vmm-id", commandConfig.VMMID, "run-cache", runCache.RunCache)
		spanFetchMetadata.Finish()
		return 1
	}

	spanFetchMetadata.Finish()

	spanInspectChroot := tracer.StartSpan("vmm-inspect-chroot", opentracing.ChildOf(spanFetchMetadata.Context()))
	spanInspectChroot.SetTag("vmm-id", vmmMetadata.VMMID)

	chrootInst := chroot.NewWithLocation(chroot.LocationFromComponents(vmmMetadata.Configs.Jailer.ChrootBase,
		vmmMetadata.Configs.Jailer.BinaryFirecracker,
		vmmMetadata.VMMID))

	chrootExists, chrootErr := chrootInst.Exists()
	if chrootErr != nil {
		rootLogger.Error("error while checking VMM chroot", "reason", chrootErr)
		spanInspectChroot.SetBaggageItem("chroot-fetch-error", chrootErr.Error())
		spanInspectChroot.Finish()
		return 1
	}

	spanInspectChroot.SetTag("chroot-existed", chrootExists)

	if !chrootExists {
		rootLogger.Error("VMM not found, nothing to do")
		spanInspectChroot.Finish()
		return 0
	}

	if err := chrootInst.IsValid(); err != nil {
		rootLogger.Error("error while inspecting chroot", "reason", err, "chroot", chrootInst.FullPath())
		spanInspectChroot.SetBaggageItem("chroot-invalid-error", err.Error())
		spanInspectChroot.Finish()
		return 1
	}

	// Do I have the socket file?
	socketPath, hasSocket, existsErr := chrootInst.SocketPathIfExists()
	if existsErr != nil {
		rootLogger.Error("failed checking if the VMM socket file exists", "reason", existsErr)
		spanInspectChroot.SetBaggageItem("chroot-socket-error", existsErr.Error())
		spanInspectChroot.Finish()
		return 1
	}

	spanInspectChroot.SetTag("has-socket", hasSocket)
	spanInspectChroot.Finish()

	if hasSocket {

		spanVMMStop := tracer.StartSpan("vmm-stop", opentracing.ChildOf(spanInspectChroot.Context()))
		spanVMMStop.SetTag("vmm-id", vmmMetadata.VMMID)

		rootLogger.Info("stopping VMM")

		spanVMMStopCall := tracer.StartSpan("vmm-stop-call", opentracing.ChildOf(spanInspectChroot.Context()))
		spanVMMStopCall.SetTag("vmm-id", vmmMetadata.VMMID)

		fcClient := firecracker.NewClient(socketPath, nil, false)
		ok, actionErr := fcClient.CreateSyncAction(context.Background(), &models.InstanceActionInfo{
			ActionType: firecracker.String("SendCtrlAltDel"),
		})

		spanVMMStopCall.Finish()

		if actionErr != nil {
			if !strings.Contains(actionErr.Error(), "connect: connection refused") {
				rootLogger.Error("failed sending CtrlAltDel to the VMM", "reason", actionErr)
				spanVMMStop.SetBaggageItem("error", actionErr.Error())
				spanVMMStop.Finish()
				return 1
			}
			rootLogger.Info("VMM is already stopped")
		} else {

			spanVMMStopWait := tracer.StartSpan("vmm-stop-wait", opentracing.ChildOf(spanVMMStopCall.Context()))
			spanVMMStopWait.SetTag("vmm-id", vmmMetadata.VMMID)

			rootLogger.Info("VMM with pid, waiting for process to exit")

			waitCtx, cancelFunc := context.WithTimeout(context.Background(), commandConfig.ShutdownTimeout)
			defer cancelFunc()
			chanErr := make(chan error, 1)

			go func() {
				chanErr <- vmmMetadata.PID.Wait(waitCtx)
			}()

			select {
			case <-waitCtx.Done():
				if waitCtx.Err() != nil {
					spanVMMStopWait.SetBaggageItem("wait-error", waitCtx.Err().Error())
				}
				spanVMMStopWait.SetTag("clean-exit", false)
				rootLogger.Error("VMM shutdown wait timed out, unclean shutdown", "reason", waitCtx.Err())
			case err := <-chanErr:
				if err != nil {
					spanVMMStopWait.SetBaggageItem("wait-error", err.Error())
					spanVMMStopWait.SetTag("clean-exit", false)
					rootLogger.Error("VMM process exit with an error", "reason", err)
				} else {
					spanVMMStopWait.SetTag("clean-exit", true)
					rootLogger.Info("VMM process exit clean")
				}
			}

			spanVMMStopWait.Finish()

			rootLogger.Info("VMM stopped with response", "response", ok)
		}

		spanVMMStop.Finish()
	}

	spanKillCNI := tracer.StartSpan("vmm-kill-cni", opentracing.ChildOf(spanInspectChroot.Context()))
	spanKillCNI.SetTag("vmm-id", vmmMetadata.VMMID)

	rootLogger.Info("cleaning up CNI")
	if err := cni.CleanupCNI(rootLogger,
		vmmMetadata.Configs.CNI,
		commandConfig.VMMID, vmmMetadata.CNI.VethIfaceName,
		vmmMetadata.CNI.NetName, vmmMetadata.CNI.NetNS); err != nil {
		rootLogger.Error("failed cleaning up CNI", "reason", err)
		spanKillCNI.SetBaggageItem("error", err.Error())
		spanKillCNI.Finish()
		return 1
	}
	rootLogger.Info("CNI cleaned up")

	spanKillCNI.Finish()

	spanKillCache := tracer.StartSpan("vmm-kill-cache", opentracing.ChildOf(spanKillCNI.Context()))
	spanKillCache.SetTag("vmm-id", vmmMetadata.VMMID)

	// have to clean up the cache
	rootLogger.Info("removing the cache directory")
	cacheDirectory := filepath.Join(runCache.RunCache, commandConfig.VMMID)
	if err := os.RemoveAll(cacheDirectory); err != nil {
		rootLogger.Error("failed removing cache directroy", "reason", err, "path", cacheDirectory)
		spanKillCache.SetBaggageItem("error", err.Error())
	}
	rootLogger.Info("cache directory removed")

	spanKillCache.Finish()

	spanKillChroot := tracer.StartSpan("vmm-kill-chroot", opentracing.ChildOf(spanKillCache.Context()))
	spanKillChroot.SetTag("vmm-id", vmmMetadata.VMMID)

	// have to clean up the jailer
	rootLogger.Info("removing the jailer directory")
	if err := chrootInst.RemoveAll(); err != nil {
		rootLogger.Error("failed removing jailer directroy", "reason", err, "path", chrootInst.FullPath())
		spanKillChroot.SetBaggageItem("error", err.Error())
	}
	rootLogger.Info("jailer directory removed")

	spanKillChroot.Finish()

	return 0
}
