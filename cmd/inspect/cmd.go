package inspect

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

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
	Use:   "inspect",
	Short: "Inspects a VMM",
	Run:   run,
	Long:  ``,
}

var (
	commandConfig  = configs.NewInspectCommandConfig()
	logConfig      = configs.NewLogginConfig()
	profilesConfig = configs.NewProfileCommandConfig()
	runCache       = configs.NewRunCacheConfig()
	tracingConfig  = configs.NewTracingConfig("firebuild-vmm-inspect")
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

	rootLogger := logConfig.NewLogger("inspect")

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

	// tracing:

	rootLogger.Info("configuring tracing", "enabled", tracingConfig.Enable, "application-name", tracingConfig.ApplicationName)

	tracer, tracerCleanupFunc, tracerErr := tracing.GetTracer(rootLogger.Named("tracer"), tracingConfig)
	if tracerErr != nil {
		rootLogger.Error("failed constructing tracer", "reason", tracerErr)
		return 1
	}

	cleanup.Add(tracerCleanupFunc)

	rootLogger, spanInspect := tracing.ApplyTraceLogDiscovery(rootLogger, tracer.StartSpan("inspect"))
	spanInspect.SetTag("vmm-id", commandConfig.VMMID)
	cleanup.Add(func() {
		spanInspect.Finish()
	})

	validatingConfigs := []configs.ValidatingConfig{
		runCache,
	}

	for _, validatingConfig := range validatingConfigs {
		if err := validatingConfig.Validate(); err != nil {
			spanInspect.SetBaggageItem("error", err.Error())
			rootLogger.Error("configuration is invalid", "reason", err)
			return 1
		}
	}

	spanFetchMetadata := tracer.StartSpan("fetch-metadata", opentracing.ChildOf(spanInspect.Context()))

	vmmMetadata, hasMetadata, metadataErr := vmm.FetchMetadataIfExists(filepath.Join(runCache.LocationRuns(), commandConfig.VMMID))
	if metadataErr != nil {
		rootLogger.Error("failed loading metadata", "reason", metadataErr, "vmm-id", commandConfig.VMMID, "run-cache", runCache.LocationRuns())
		spanFetchMetadata.SetBaggageItem("error", metadataErr.Error())
		spanFetchMetadata.Finish()
		return 1
	}

	spanFetchMetadata.SetTag("has-metadata", hasMetadata)

	if !hasMetadata {
		rootLogger.Error("run cache directory did not contain the VMM metadata", "vmm-id", commandConfig.VMMID, "run-cache", runCache.LocationRuns())
		spanFetchMetadata.Finish()
		return 1
	}

	spanFetchMetadata.Finish()

	spanMarshalMetadata := tracer.StartSpan("marshal-metadata", opentracing.ChildOf(spanFetchMetadata.Context()))

	bytes, jsonErr := json.MarshalIndent(vmmMetadata, "", "  ")
	if jsonErr != nil {
		rootLogger.Error("failed serializing VMM metadata to JSON", "vmm-id", commandConfig.VMMID, "run-cache", runCache.LocationRuns(), "reason", jsonErr)
		spanFetchMetadata.SetBaggageItem("error", jsonErr.Error())
		spanMarshalMetadata.Finish()
		return 1
	}

	spanMarshalMetadata.Finish()

	fmt.Println(string(bytes))

	return 0

}
