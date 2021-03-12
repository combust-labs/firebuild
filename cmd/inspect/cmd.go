package inspect

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/combust-labs/firebuild/configs"
	"github.com/combust-labs/firebuild/pkg/utils"
	"github.com/combust-labs/firebuild/pkg/vmm"
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
	commandConfig = configs.NewInspectCommandConfig()
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

	rootLogger := logConfig.NewLogger("inspect")

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

	bytes, jsonErr := json.MarshalIndent(vmmMetadata, "", "  ")
	if jsonErr != nil {
		rootLogger.Error("failed serializing VMM metadata to JSON", "vmm-id", commandConfig.VMMID, "run-cache", runCache.RunCache, "reason", jsonErr)
		os.Exit(1)
	}

	fmt.Println(string(bytes))

}
