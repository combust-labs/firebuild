package ls

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"github.com/combust-labs/firebuild/configs"
	"github.com/combust-labs/firebuild/pkg/profiles"
	"github.com/combust-labs/firebuild/pkg/tracing"
	"github.com/combust-labs/firebuild/pkg/utils"
	"github.com/combust-labs/firebuild/pkg/vmm"
	"github.com/opentracing/opentracing-go"
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
	logConfig      = configs.NewLogginConfig()
	profilesConfig = configs.NewProfileCommandConfig()
	runCache       = configs.NewRunCacheConfig()
	tracingConfig  = configs.NewTracingConfig("firebuild-vmm-ls")
)

func initFlags() {
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

	rootLogger := logConfig.NewLogger("ls")

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

	rootLogger, spanLs := tracing.ApplyTraceLogDiscovery(rootLogger, tracer.StartSpan("ls"))
	cleanup.Add(func() {
		spanLs.Finish()
	})

	itemsWithMetadata := 0
	itemsWithoutMetadata := 0

	fileInfos, readDirErr := ioutil.ReadDir(runCache.LocationRuns())
	if readDirErr != nil {
		rootLogger.Error("error listing run cache directory", "reason", readDirErr)
	}
	for _, fileInfo := range fileInfos {

		// see if the pid file can be loaded:
		vmmID := fileInfo.Name()

		spanVMM := tracer.StartSpan("vmm-fetch-metadata", opentracing.ChildOf(spanLs.Context()))
		spanVMM.SetTag("vmm-id", vmmID)

		vmmMetadata, hasMetadata, err := vmm.FetchMetadataIfExists(filepath.Join(runCache.LocationRuns(), vmmID))
		if err != nil {
			rootLogger.Error("failed loading metadata file for possible VMM", "vmm-id", vmmID, "reason", err)
			spanVMM.SetBaggageItem("error", err.Error())
			spanVMM.Finish()
			continue
		}

		spanVMM.SetTag("has-metadata", hasMetadata)
		spanVMM.Finish()

		if hasMetadata {

			spanVMMPID := tracer.StartSpan("vmm-pid-check", opentracing.ChildOf(spanVMM.Context()))

			itemsWithMetadata = itemsWithMetadata + 1
			running, err := vmmMetadata.PID.IsRunning()
			if err != nil {
				rootLogger.Error("failed checking pid status for possible VMM", "vmm-id", vmmID, "reason", err)
				spanVMMPID.SetBaggageItem("error", err.Error())
				spanVMMPID.Finish()
				continue
			}

			rootLogger.Info("vmm", "id", vmmID,
				"running", running,
				"pid", vmmMetadata.PID.Pid,
				"image", fmt.Sprintf("%s/%s:%s", vmmMetadata.Rootfs.Image.Org, vmmMetadata.Rootfs.Image.Image, vmmMetadata.Rootfs.Image.Version),
				"started", time.Unix(vmmMetadata.StartedAtUTC, 0).UTC().String(),
				"ip-address", vmmMetadata.NetworkInterfaces[0].StaticConfiguration.IPConfiguration.IP)

			spanVMMPID.SetTag("is-running", running)
			spanVMMPID.Finish()

		} else {
			itemsWithoutMetadata = itemsWithoutMetadata + 1
			rootLogger.Info("vmm", "id", vmmID, "running", "???", "pid", "???")
		}

	}

	spanLs.SetBaggageItem("with-metadata", fmt.Sprintf("%d", itemsWithMetadata))
	spanLs.SetBaggageItem("without-metadata", fmt.Sprintf("%d", itemsWithoutMetadata))

	return 0
}
