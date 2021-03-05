package baseos

import (
	"fmt"
	"io/fs"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/combust-labs/firebuild/build"
	"github.com/combust-labs/firebuild/build/commands"
	"github.com/combust-labs/firebuild/build/reader"
	"github.com/combust-labs/firebuild/build/stage"
	"github.com/combust-labs/firebuild/build/utils"
	"github.com/combust-labs/firebuild/configs"
	"github.com/spf13/cobra"
)

/*
	sudo /usr/local/go/bin/go run ./main.go baseos --dockerfile $(pwd)/baseos/_/alpine/3.13/Dockerfile
	the authorized keys file must be 0400
*/

// Command is the build command declaration.
var Command = &cobra.Command{
	Use:   "baseos",
	Short: "Build a base operating system image",
	Run:   run,
	Long:  ``,
}

type buildConfig struct {
	Dockerfile string
	FSSizeMBs  int
}

var (
	commandConfig = new(buildConfig)
	logConfig     = new(configs.LogConfig)
)

func initFlags() {
	Command.Flags().StringVar(&commandConfig.Dockerfile, "dockerfile", "", "Full path to the base OS Dockerfile")
	Command.Flags().IntVar(&commandConfig.FSSizeMBs, "filesystem-size-mbs", 500, "File system size in megabytes")
	// Log settings:
	Command.Flags().StringVar(&logConfig.LogLevel, "log-level", "debug", "Log level")
	Command.Flags().BoolVar(&logConfig.LogAsJSON, "log-as-json", false, "Log as JSON")
	Command.Flags().BoolVar(&logConfig.LogColor, "log-color", false, "Log in color")
	Command.Flags().BoolVar(&logConfig.LogForceColor, "log-force-color", false, "Force colored log output")
}

func init() {
	initFlags()
}

func run(cobraCommand *cobra.Command, _ []string) {

	rootLogger := configs.NewLogger("baseos", logConfig)

	dockerStat, statErr := os.Stat(commandConfig.Dockerfile)
	if statErr != nil {
		rootLogger.Error("error while resolving --dockerfile path", "reason", statErr)
		os.Exit(1)
	}
	if dockerStat.IsDir() {
		rootLogger.Error("--dockerfile points at a directory")
		os.Exit(1)
	}

	tempDirectory, err := ioutil.TempDir("", "")
	if err != nil {
		rootLogger.Error("Failed creating temporary build directory", "reason", err)
		os.Exit(1)
	}

	// we parse the file to establish the base operating system we build
	// we must enforce some constants so we assume here
	// no multi-stage builds - only main stage
	readResults, err := reader.ReadFromString(commandConfig.Dockerfile, tempDirectory)
	if err != nil {
		rootLogger.Error("Failed parsing Dockerfile", "reason", err)
		os.RemoveAll(tempDirectory)
		os.Exit(1)
	}

	scs, errs := stage.ReadStages(readResults.Commands())
	for _, err := range errs {
		rootLogger.Warn("Stages read contained an error", "reason", err)
	}

	if len(scs.Unnamed()) != 1 {
		rootLogger.Error("Dockerfile must contain exactly one unnamed FROM build stage")
		os.RemoveAll(tempDirectory)
		os.Exit(1)
	}

	if len(scs.Named()) > 0 {
		rootLogger.Error("Dockerfile contains other named stages, this is not supported.")
		os.RemoveAll(tempDirectory)
		os.Exit(1)
	}

	// find out what OS are we building:
	var osToBuild string
	for _, cmd := range scs.Unnamed()[0].Commands() {
		switch tcmd := cmd.(type) {
		case commands.From:
			osToBuild = tcmd.BaseImage
			break
		}
	}

	rootLogger.Info("We are building", "os", osToBuild)

	// we have to build the Docker image, we can use the dependency builder here:
	builder, builderErr := build.NewDefaultDependencyBuild(scs.Unnamed()[0], tempDirectory, "" /* irrelevant */)
	if builderErr != nil {
		rootLogger.Error("Failed creating the builder", "reason", builderErr)
		os.RemoveAll(tempDirectory)
		os.Exit(1)
	}

	tagName := strings.ToLower(utils.RandStringBytes(32)) + ":build"

	builder = builder.WithLogger(rootLogger)

	if err := builder.ImageBuild(filepath.Dir(commandConfig.Dockerfile), "Dockerfile", tagName); err != nil {
		rootLogger.Error("Failed building base OS Docker image", "reason", err)
		os.RemoveAll(tempDirectory)
		os.Exit(1)
	}

	if _, findErr := builder.FindImageIDByTag(tagName); findErr != nil {
		// be extra careful:
		rootLogger.Error("Expected docker image not found", "reason", findErr)
		os.RemoveAll(tempDirectory)
		os.Exit(1)
	}

	rootFSFile := fmt.Sprintf("%s/rootfs.ext4", tempDirectory)
	if err := createRootFSFile(rootFSFile, commandConfig.FSSizeMBs); err != nil {
		rootLogger.Error("Failed creating rootfs file", "reason", err)
		os.RemoveAll(tempDirectory)
		os.Exit(1)
	}
	rootLogger.Info("EXT4 file created", "path", rootFSFile, "size-mb", commandConfig.FSSizeMBs)

	if err := mkfsExt4(rootFSFile); err != nil {
		rootLogger.Error("Failed creating EXT4 in rootfs file", "reason", err)
		os.RemoveAll(tempDirectory)
		os.Exit(1)
	}
	rootLogger.Info("EXT4 filesystem created", "path", rootFSFile, "size-mb", commandConfig.FSSizeMBs)

	// create the mount directory:
	mountDir := filepath.Join(tempDirectory, "mount")
	mkdirErr := os.Mkdir(mountDir, fs.ModePerm)
	if mkdirErr != nil {
		rootLogger.Error("Failed creating EXT4 mount directory", "reason", mkdirErr)
		os.RemoveAll(tempDirectory)
		os.Exit(1)
	}

	if err := mount(rootFSFile, mountDir); err != nil {
		rootLogger.Error("Failed mounting rootfs file in mount dir", "reason", err)
		os.RemoveAll(tempDirectory)
		os.Exit(1)
	}
	rootLogger.Info("rootfs file mounted in mount dir", "rootfs", rootFSFile, "mount-dir", mountDir)

	builder.ImageBaseOSExport(mountDir, tagName)

	if err := umount(mountDir); err != nil {
		rootLogger.Error("Failed unmounting rootfs mount dir", "reason", err)
		os.RemoveAll(tempDirectory)
		os.Exit(1)
	}
	rootLogger.Info("rootfs file unmounted from mount dir", "rootfs", rootFSFile, "mount-dir", mountDir)

	_, cmdErr := runShellCommandNoSudo(fmt.Sprintf("mv %s /tmp/rootfs.test", rootFSFile))
	if cmdErr != nil {
		rootLogger.Error("Failed moving produced file system", "reason", cmdErr)
	}

	if err := builder.ImageRemove(tagName); err != nil {
		rootLogger.Error("Failed post-build image clean up", "reason", err)
		os.RemoveAll(tempDirectory)
		os.Exit(1)
	}

	os.RemoveAll(tempDirectory)

}

func createRootFSFile(path string, size int) error {
	exitCode, cmdErr := runShellCommandNoSudo(fmt.Sprintf("dd if=/dev/zero of=%s bs=1M count=%d", path, size))
	if cmdErr != nil {
		return cmdErr
	}
	if exitCode != 0 {
		return fmt.Errorf("coomand finished with non-zero exit code")
	}
	return nil
}

func mkfsExt4(path string) error {
	exitCode, cmdErr := runShellCommandNoSudo(fmt.Sprintf("mkfs.ext4 %s", path))
	if cmdErr != nil {
		return cmdErr
	}
	if exitCode != 0 {
		return fmt.Errorf("coomand finished with non-zero exit code")
	}
	return nil
}

func mount(file, dir string) error {
	exitCode, cmdErr := runShellCommand(fmt.Sprintf("mount %s %s", file, dir), true)
	if cmdErr != nil {
		return cmdErr
	}
	if exitCode != 0 {
		return fmt.Errorf("command finished with non-zero exit code")
	}
	return nil
}

func umount(dir string) error {
	exitCode, cmdErr := runShellCommand(fmt.Sprintf("umount %s", dir), true)
	if cmdErr != nil {
		return cmdErr
	}
	if exitCode != 0 {
		return fmt.Errorf("command finished with non-zero exit code")
	}
	return nil
}

func runShellCommandNoSudo(command string) (int, error) {
	return runShellCommand(command, false)
}

func runShellCommand(command string, sudo bool) (int, error) {
	if sudo {
		command = fmt.Sprintf("sudo %s", command)
	}
	cmd := exec.Command("/bin/sh", []string{`-c`, command}...)
	cmd.Stderr = os.Stderr
	stdOut, err := cmd.StdoutPipe()
	if err != nil {
		return 1, fmt.Errorf("failed redirecting stdout: %+v", err)
	}
	if err := cmd.Start(); err != nil {
		return 1, fmt.Errorf("failed command start: %+v", err)
	}
	_, readErr := ioutil.ReadAll(stdOut)
	if readErr != nil {
		return 1, fmt.Errorf("failed reading output: %+v", readErr)
	}
	if err := cmd.Wait(); err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			return exitError.ExitCode(), exitError
		}
		return 1, fmt.Errorf("failed waiting for command: %+v", err)
	}
	return 0, nil
}