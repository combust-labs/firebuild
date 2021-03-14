package create

import (
	"os"

	"github.com/combust-labs/firebuild/configs"
	"github.com/combust-labs/firebuild/pkg/profiles"
	"github.com/combust-labs/firebuild/pkg/utils"
	"github.com/spf13/cobra"
)

/*
sudo /usr/local/go/bin/go run ./main.go profile-ls
*/

// Command is the build command declaration.
var Command = &cobra.Command{
	Use:   "profile-ls",
	Short: "List profiles",
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

	rootLogger := logConfig.NewLogger("profile-ls")

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

	profiles, err := profiles.ListProfiles(profileSelectionConfig.ProfileConfDir)
	if err != nil {
		rootLogger.Error("failed listing profiles", "reason", err)
		return 1
	}

	for _, p := range profiles {
		rootLogger.Info(p)
	}

	return 0

}
