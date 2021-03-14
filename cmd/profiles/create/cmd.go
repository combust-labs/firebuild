package create

import (
	"os"

	"github.com/combust-labs/firebuild/configs"
	"github.com/combust-labs/firebuild/pkg/profiles"
	"github.com/combust-labs/firebuild/pkg/utils"
	"github.com/spf13/cobra"
)

/*
sudo /usr/local/go/bin/go run ./main.go profile-create \
	--profile=standard \
	--binary-firecracker=$(readlink /usr/bin/firecracker) \
	--binary-jailer=$(readlink /usr/bin/jailer) \
	--chroot-base=/srv/jailer \
	--run-cache=/var/lib/firebuild/runs \
	--storage-provider=directory \
	--storage-provider-property-string="rootfs-storage-root=/firecracker/rootfs" \
	--storage-provider-property-string="kernel-storage-root=/firecracker/vmlinux" \
	--tracing-enable
*/

// Command is the build command declaration.
var Command = &cobra.Command{
	Use:   "profile-create",
	Short: "Create a firebuild profile",
	Run:   run,
	Long:  ``,
}

var (
	profileSelectionConfig = configs.NewProfileCommandConfig()
	profileCreateConfig    = configs.NewProfileCreateConfig()
	logConfig              = configs.NewLogginConfig()
)

func initFlags() {
	Command.Flags().AddFlagSet(profileSelectionConfig.FlagSet())
	Command.Flags().AddFlagSet(profileCreateConfig.FlagSet())
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

	rootLogger := logConfig.NewLogger("profile-create")

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
		profileCreateConfig,
	}

	for _, validatingConfig := range validatingConfigs {
		if err := validatingConfig.Validate(); err != nil {
			rootLogger.Error("profile configuration invalid", "reason", err)
			return 1
		}
	}

	if err := profiles.WriteProfileFile(profileSelectionConfig.Profile, profileSelectionConfig.ProfileConfDir, profileCreateConfig); err != nil {
		rootLogger.Error("profile not created", "reason", err)
		return 1
	}

	rootLogger.Info("profile created")

	return 0

}
