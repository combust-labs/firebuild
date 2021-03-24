package purge

import (
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/combust-labs/firebuild/configs"
	"github.com/combust-labs/firebuild/pkg/profiles"
	"github.com/combust-labs/firebuild/pkg/tracing"
	"github.com/combust-labs/firebuild/pkg/utils"
	"github.com/combust-labs/firebuild/pkg/vmm"
	"github.com/combust-labs/firebuild/pkg/vmm/chroot"
	"github.com/combust-labs/firebuild/pkg/vmm/cni"
	"github.com/opentracing/opentracing-go"
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
	logConfig      = configs.NewLogginConfig()
	profilesConfig = configs.NewProfileCommandConfig()
	runCache       = configs.NewRunCacheConfig()
	tracingConfig  = configs.NewTracingConfig("firebuild-vmm-purge")
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

	rootLogger := logConfig.NewLogger("purge")

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

	rootLogger = rootLogger.With("run-cache", runCache.LocationRuns())

	// tracing:

	rootLogger.Trace("configuring tracing", "enabled", tracingConfig.Enable, "application-name", tracingConfig.ApplicationName)

	tracer, tracerCleanupFunc, tracerErr := tracing.GetTracer(rootLogger.Named("tracer"), tracingConfig)
	if tracerErr != nil {
		rootLogger.Error("failed constructing tracer", "reason", tracerErr)
		return 1
	}

	cleanup.Add(tracerCleanupFunc)

	rootLogger, spanPurge := tracing.ApplyTraceLogDiscovery(rootLogger, tracer.StartSpan("purge"))
	cleanup.Add(func() {
		spanPurge.Finish()
	})

	validatingConfigs := []configs.ValidatingConfig{
		runCache,
	}

	for _, validatingConfig := range validatingConfigs {
		if err := validatingConfig.Validate(); err != nil {
			spanPurge.SetBaggageItem("error", err.Error())
			rootLogger.Error("configuration is invalid", "reason", err)
			return 1
		}
	}

	fileInfos, readDirErr := ioutil.ReadDir(runCache.LocationRuns())
	if readDirErr != nil {
		rootLogger.Error("error listing run cache directory", "reason", readDirErr)
		spanPurge.SetBaggageItem("error", readDirErr.Error())
		return 1
	}

	for _, fileInfo := range fileInfos {

		// see if the metadata file can be loaded:
		fsentry := fileInfo.Name()

		spanMetadata := tracer.StartSpan("vmm-fetch-metadata", opentracing.ChildOf(spanPurge.Context()))
		spanMetadata.SetTag("fs-entry", fsentry)

		vmmMetadata, hasMetadata, err := vmm.FetchMetadataIfExists(filepath.Join(runCache.LocationRuns(), fsentry))
		if err != nil {
			rootLogger.Error("metadata error for cache entry, skipping", "fs-entry", fsentry, "reason", err)
			spanMetadata.SetBaggageItem("error", err.Error())
			spanMetadata.Finish()
			continue
		}

		spanMetadata.SetTag("has-metadata", hasMetadata)
		spanMetadata.Finish()

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

			spanPurgeChroot := tracer.StartSpan("vmm-purge-chroot", opentracing.ChildOf(spanMetadata.Context()))
			spanPurgeChroot.SetTag("fs-entry", fsentry)

			// get the chroot:
			chrootInst := chroot.NewWithLocation(chroot.LocationFromComponents(vmmMetadata.Configs.Jailer.ChrootBase,
				vmmMetadata.Configs.Jailer.BinaryFirecracker,
				vmmMetadata.VMMID))
			chrootExists, chrootErr := chrootInst.Exists()

			if chrootErr != nil {
				spanPurgeChroot.SetBaggageItem("chroot-fetch-error", chrootErr.Error())
				vmmLogger.Error("error while checking VMM chroot, skipping", "reason", chrootErr)
			}

			spanPurgeChroot.SetTag("chroot-existed", chrootExists)

			if chrootErr == nil && chrootExists {
				if err := chrootInst.RemoveAll(); err != nil {
					spanPurgeChroot.SetBaggageItem("chroot-purge-error", err.Error())
					vmmLogger.Error("error removing chroot directory fro stopped VMM", "reason", err)
				}
			}

			spanPurgeChroot.Finish()

			spanPurgeCNI := tracer.StartSpan("vmm-purge-cni", opentracing.ChildOf(spanPurgeChroot.Context()))
			spanPurgeCNI.SetTag("fs-entry", fsentry)

			if err := cni.CleanupCNI(rootLogger,
				vmmMetadata.Configs.CNI,
				vmmMetadata.VMMID, vmmMetadata.CNI.VethIfaceName,
				vmmMetadata.CNI.NetName, vmmMetadata.CNI.NetNS); err != nil {
				spanPurgeCNI.SetBaggageItem("cni-purge-error", err.Error())
				vmmLogger.Error("failed cleaning up CNI", "reason", err)
			}

			spanPurgeCNI.Finish()

			spanPurgeCache := tracer.StartSpan("vmm-purge-cache", opentracing.ChildOf(spanPurgeCNI.Context()))
			spanPurgeCache.SetTag("fs-entry", fsentry)

			// have to clean up the cache
			cacheDirectory := filepath.Join(runCache.LocationRuns(), vmmMetadata.VMMID)
			if err := os.RemoveAll(cacheDirectory); err != nil {
				spanPurgeCache.SetBaggageItem("cache-purge-error", err.Error())
				vmmLogger.Error("failed removing cache directroy", "reason", err, "path", cacheDirectory)
			}

			spanPurgeCache.Finish()

			vmmLogger.Info(vmmMetadata.VMMID)

		} else {
			rootLogger.Warn("no metadata for entry, skipping", "fs-entry", fsentry)
		}
	}

	return 0
}
