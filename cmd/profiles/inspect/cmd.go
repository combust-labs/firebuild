package inspect

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/combust-labs/firebuild/configs"
	"github.com/combust-labs/firebuild/pkg/profiles"
	"github.com/combust-labs/firebuild/pkg/utils"
	"github.com/spf13/cobra"
)

/*
sudo /usr/local/go/bin/go run ./main.go profile-inspect \
	--profile=standard
*/

// Command is the build command declaration.
var Command = &cobra.Command{
	Use:   "profile-inspect",
	Short: "Inspect a firebuild profile",
	Run:   run,
	Long:  ``,
}

var (
	profileSelectionConfig = configs.NewProfileCommandConfig()
	logConfig              = configs.NewLogginConfig()
)

func initFlags() {
	Command.Flags().AddFlagSet(profileSelectionConfig.FlagSet())
	Command.Flags().AddFlagSet(logConfig.FlagSet())
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

	rootLogger := logConfig.NewLogger("profile-inspect")

	if _, err := utils.CheckIfExistsAndIsDirectory(profileSelectionConfig.ProfileConfDir); err != nil {
		if !os.IsNotExist(err) {
			rootLogger.Error("error validating profile configuration directory", "reason", err)
			return 1
		}
		// make sure the directory exists:
		if err := os.MkdirAll(profileSelectionConfig.ProfileConfDir, 0644); err != nil {
			rootLogger.Error("error creating profile configuration directory", "reason", err)
			return 1
		}
	}

	validatingConfigs := []configs.ValidatingConfig{
		profileSelectionConfig,
	}

	for _, validatingConfig := range validatingConfigs {
		if err := validatingConfig.Validate(); err != nil {
			rootLogger.Error("profile configuration invalid", "reason", err)
			return 1
		}
	}

	profile, err := profiles.ReadProfile(profileSelectionConfig.Profile, profileSelectionConfig.ProfileConfDir)
	if err != nil {
		rootLogger.Error("profile inspect failed", "reason", err)
		return 1
	}

	bytes, jsonErr := json.MarshalIndent(profile, "", "  ")
	if jsonErr != nil {
		rootLogger.Error("profile inspect failed", "reason", jsonErr)
		return 1
	}

	fmt.Println(string(bytes))

	return 0

}
