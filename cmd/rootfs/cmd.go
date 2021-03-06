package rootfs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"github.com/combust-labs/firebuild/build"
	"github.com/combust-labs/firebuild/build/commands"
	"github.com/combust-labs/firebuild/build/reader"
	"github.com/combust-labs/firebuild/build/resources"
	"github.com/combust-labs/firebuild/build/stage"
	"github.com/combust-labs/firebuild/build/utils"
	"github.com/combust-labs/firebuild/configs"
	"github.com/combust-labs/firebuild/remote"
	"github.com/combust-labs/firebuild/strategy"
	firecracker "github.com/firecracker-microvm/firecracker-go-sdk"
	"github.com/hashicorp/go-hclog"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh"

	"github.com/sirupsen/logrus"

	"github.com/containernetworking/cni/libcni"
)

type stoppedOK = bool

// Command is the build command declaration.
var Command = &cobra.Command{
	Use:   "rootfs",
	Short: "Build A VMM root file system from a Docker image",
	Run:   run,
	Long:  ``,
}

type buildConfig struct {
	BuildArgs                      map[string]string
	Dockerfile                     string
	PostBuildCommands              []string
	PreBuildCommands               []string
	ServiceFileInstaller           string
	ShutdownGracefulTimeoutSeconds int
	Tag                            string
}

var (
	cniConfig        = configs.NewCNIConfig()
	egressTestConfig = configs.NewEgressTestConfig()
	jailingFcConfig  = configs.NewJailingFirecrackerConfig()
	logConfig        = configs.NewLogginConfig()
	machineConfig    = configs.NewMachineConfig()

	commandConfig        = new(buildConfig)
	rootFSCopyBufferSize = 4 * 1024 * 1024
	stoppedGracefully    = stoppedOK(true)
	stoppedForcefully    = stoppedOK(false)
	rsaKeySize           = 4096
)

func initFlags() {

	Command.Flags().AddFlagSet(cniConfig.FlagSet())
	Command.Flags().AddFlagSet(egressTestConfig.FlagSet())
	Command.Flags().AddFlagSet(jailingFcConfig.FlagSet())
	Command.Flags().AddFlagSet(logConfig.FlagSet())
	Command.Flags().AddFlagSet(machineConfig.FlagSet())

	// Bootstrap settings:
	Command.Flags().StringToStringVar(&commandConfig.BuildArgs, "build-arg", map[string]string{}, "Build arguments, Multiple OK")
	Command.Flags().StringVar(&commandConfig.Dockerfile, "dockerfile", "", "Local or remote (HTTP / HTTP) path; if the Dockerfile uses ADD or COPY commands, it's recommended to use a local file")
	Command.Flags().StringArrayVar(&commandConfig.PostBuildCommands, "post-build-command", []string{}, "OS specific commands to run after Dockerfile commands but before the file system is persisted, multiple OK")
	Command.Flags().StringArrayVar(&commandConfig.PreBuildCommands, "pre-build-command", []string{}, "OS specific commands to run before any Dockerfile command, multiple OK")
	Command.Flags().StringVar(&commandConfig.ServiceFileInstaller, "service-file-installer", "", "Local path to the program to upload to the VMM and install the service file")
	Command.Flags().IntVar(&commandConfig.ShutdownGracefulTimeoutSeconds, "shutdown-graceful-timeout-seconds", 30, "Grafeul shotdown timeout before vmm is stopped forcefully")
	Command.Flags().StringVar(&commandConfig.Tag, "tag", "", "Tag name of the build, required; must be org/name:version")
}

func init() {
	initFlags()
}

func run(cobraCommand *cobra.Command, _ []string) {

	cleanup := &defers{fs: []func(){}}
	defer cleanup.exec()

	rootLogger := logConfig.NewLogger("rootfs")

	if machineConfig.MachineRootFSBase == "" || machineConfig.MachineRootFSBase == "/" {
		rootLogger.Error("--machine-rootfs-base is empty or /")
		os.Exit(1)
	}

	if commandConfig.Tag == "" {
		rootLogger.Error("--tag is required")
		os.Exit(1)
	}

	if !isTagValid(commandConfig.Tag) {
		rootLogger.Error("--tag value is invalid", "tag", commandConfig.Tag)
		os.Exit(1)
	}

	tempDirectory, err := ioutil.TempDir("", "")
	if err != nil {
		rootLogger.Error("Failed creating temporary build directory", "reason", err)
		os.Exit(1)
	}

	readResults, err := reader.ReadFromString(commandConfig.Dockerfile, tempDirectory)
	if err != nil {
		rootLogger.Error("failed parsing Dockerfile", "reason", err)
		os.Exit(1)
	}

	scs, errs := stage.ReadStages(readResults.Commands())
	for _, err := range errs {
		rootLogger.Warn("stages read contained an error", "reason", err)
	}

	unnamed := scs.Unnamed()
	if len(unnamed) != 1 {
		rootLogger.Error("expected exactly one unnamed build stage but found", "num-unnamed", len(unnamed))
		os.Exit(1)
	}

	mainStage := unnamed[0]
	if !mainStage.IsValid() {
		rootLogger.Error("main build stage invalid: no base to build from")
		cleanup.exec() // manually - defers don't run on os.Exit
		os.Exit(1)
	}

	rsaPrivateKey, rsaPrivateKeyErr := utils.GenerateRSAPrivateKey(rsaKeySize)
	if rsaPrivateKeyErr != nil {
		rootLogger.Error("failed generating private key for the VMM build", "reason", rsaPrivateKeyErr)
		cleanup.exec() // manually - defers don't run on os.Exit
		os.Exit(1)
	}
	sshPublicKey, sshPublicKeyErr := utils.GetSSHKey(rsaPrivateKey)
	if sshPublicKeyErr != nil {
		rootLogger.Error("failed generating public SSH key from the private RSA key", "reason", sshPublicKeyErr)
		cleanup.exec() // manually - defers don't run on os.Exit
		os.Exit(1)
	}

	// The first thing to do is to resolve the Dockerfile:
	buildContext := build.NewDefaultBuild()
	if err := buildContext.AddInstructions(unnamed[0].Commands()...); err != nil {
		rootLogger.Error("commands could not be processed", "reason", err)
		cleanup.exec() // manually - defers don't run on os.Exit
		os.Exit(1)
	}

	structuredBase := buildContext.From().ToStructuredFrom()

	cleanup.add(func() {
		rootLogger.Info("cleaning up temp build directory")
		status := os.RemoveAll(tempDirectory)
		rootLogger.Info("temp build directory removal status", "error", status)
	})

	// TODO: check that it exists and is regular file
	sourceRootfs := filepath.Join(machineConfig.MachineRootFSBase, structuredBase.Org(), structuredBase.OS(), structuredBase.Version(), "root.ext4")
	buildRootfs := filepath.Join(tempDirectory, "rootfs")

	// which resources from dependencies do we need:
	requiredCopies := []commands.Copy{}
	for _, stage := range scs.All() {
		for _, stageCommand := range stage.Commands() {
			switch tcommand := stageCommand.(type) {
			case commands.Copy:
				if tcommand.Stage != "" {
					requiredCopies = append(requiredCopies, tcommand)
				}
			}
		}
	}

	// resolve dependencies:
	dependencyResources := map[string][]resources.ResolvedResource{}
	for _, stage := range scs.All() {
		for _, dependency := range stage.DependsOn() {
			if _, ok := dependencyResources[dependency]; !ok {
				dependencyStage := scs.NamedStage(dependency)
				if dependencyStage == nil {
					rootLogger.Error("main build stage depends on non-existent stage", "dependency", dependency)
					cleanup.exec() // manually - defers don't run on os.Exit
					os.Exit(1)
				}
				dependencyBuilder := build.NewDefaultDependencyBuild(dependencyStage, tempDirectory, filepath.Join(tempDirectory, "sources"))
				resolvedResources, buildError := dependencyBuilder.Build(requiredCopies)
				if buildError != nil {
					rootLogger.Error("failed building stage dependency", "stage", stage.Name(), "dependency", dependency, "reason", buildError)
					cleanup.exec() // manually - defers don't run on os.Exit
					os.Exit(1)
				}
				dependencyResources[dependency] = resolvedResources
			}
		}
	}

	if err := copyFile(sourceRootfs, buildRootfs, rootFSCopyBufferSize); err != nil {
		rootLogger.Error("failed copying requested rootfs to temp build location",
			"source", sourceRootfs,
			"target", buildRootfs,
			"reason", err)
		cleanup.exec() // manually - defers don't run on os.Exit
		os.Exit(1)
	}

	vethIfaceName := getRandomVethName()

	vmmLogger := rootLogger.With("vmm-id", jailingFcConfig.VMMID(), "veth-name", vethIfaceName)

	vmmLogger.Info("buildiing VMM",
		"dockerfile", commandConfig.Dockerfile,
		"source-rootfs", buildRootfs,
		"origin-rootfs", sourceRootfs,
		"jail", jailingFcConfig.JailerChrootDirectory())

	cleanup.add(func() {
		vmmLogger.Info("cleaning up jail directory")
		status := os.RemoveAll(jailingFcConfig.JailerChrootDirectory())
		vmmLogger.Info("jail directory removal status", "error", status)
	})

	strategyConfig := &strategy.SSHKeyInjectingHandlerConfig{
		Chroot:         jailingFcConfig.JailerChrootDirectory(),
		RootfsFileName: filepath.Base(buildRootfs),
		SSHUser:        machineConfig.MachineSSHUser,
		PublicKeys: []ssh.PublicKey{
			sshPublicKey,
		},
	}

	strategy := strategy.NewSSHKeyInjectingStrategy(rootLogger, strategyConfig, func() strategy.HandlerWithRequirement {
		return strategy.HandlerWithRequirement{
			AppendAfter: firecracker.CreateLogFilesHandlerName,
			// replicate what firecracker.NaiveChrootStrategy is doing...
			Handler: firecracker.LinkFilesHandler(filepath.Base(machineConfig.MachineVMLinux)),
		}
	})

	fcConfig := configs.NewFcConfigProvider(jailingFcConfig, machineConfig).
		WithHandlersAdapter(strategy).
		WithVethIfaceName(vethIfaceName).
		WithRootFsHostPath(buildRootfs).
		ToSDKConfig()

	vmmCtx, vmmCancel := context.WithCancel(context.Background())
	cleanup.add(func() {
		vmmCancel()
	})

	machine, runErr := runVMM(vmmCtx, fcConfig)
	if runErr != nil {
		vmmLogger.Error("Firecracker VMM did not start, build failed", "reason", runErr)
		return
	}

	ifaceStaticConfig := fcConfig.NetworkInterfaces[0].StaticConfiguration

	vmmLogger = vmmLogger.With("ip-address", ifaceStaticConfig.IPConfiguration.IPAddr.IP.String())

	vmmLogger.Info("VMM running", "ip-net", ifaceStaticConfig.IPConfiguration.IPAddr.String())

	remoteClient, remoteErr := remote.Connect(context.Background(), remote.ConnectConfig{
		SSHPrivateKey:      *rsaPrivateKey,
		SSHUsername:        machineConfig.MachineSSHUser,
		IP:                 ifaceStaticConfig.IPConfiguration.IPAddr.IP,
		Port:               machineConfig.MachineSSHPort,
		EnableAgentForward: machineConfig.MachineSSHEnableAgentForward,
	}, vmmLogger.Named("remote-client"))

	if remoteErr != nil {
		stopVMM(vmmCtx, machine, vethIfaceName, nil, vmmLogger)
		vmmLogger.Error("Failed connecting to remote", "reason", remoteErr)
		return
	}

	vmmLogger.Info("Connected via SSH")

	if !egressTestConfig.EgressNoWait {
		egressCtx, egressWaitCancelFunc := context.WithTimeout(vmmCtx, time.Second*time.Duration(egressTestConfig.EgressTestTimeoutSeconds))
		defer egressWaitCancelFunc()
		if err := remoteClient.WaitForEgress(egressCtx, egressTestConfig.EgressTestTarget); err != nil {
			stopVMM(vmmCtx, machine, vethIfaceName, remoteClient, vmmLogger)
			vmmLogger.Error("Did not get egress connectivity within a timeout", "reason", err)
			return
		}
	} else {
		vmmLogger.Debug("Egress test explicitly skipped")
	}

	postBuildCommands := []commands.Run{}
	for _, cmd := range commandConfig.PostBuildCommands {
		postBuildCommands = append(postBuildCommands, commands.RunWithDefaults(cmd))
	}
	preBuildCommands := []commands.Run{}
	for _, cmd := range commandConfig.PreBuildCommands {
		preBuildCommands = append(preBuildCommands, commands.RunWithDefaults(cmd))
	}

	if buildErr := buildContext.
		WithLogger(vmmLogger.Named("builder")).
		WithServiceInstaller(commandConfig.ServiceFileInstaller).
		WithPostBuildCommands(postBuildCommands...).
		WithPreBuildCommands(preBuildCommands...).
		WithDependencyResources(dependencyResources).
		Build(remoteClient); err != nil {
		stopVMM(vmmCtx, machine, vethIfaceName, remoteClient, vmmLogger)
		vmmLogger.Error("Failed boostrapping remote via SSH", "reason", buildErr)
		return
	}

	go func() {
		if stopVMM(vmmCtx, machine, vethIfaceName, remoteClient, vmmLogger) == stoppedForcefully {
			vmmLogger.Warn("Machine was not stopped gracefully, see previous errors. It's possible that the file system may not be complete. Retry or proceed with caution.")
		}
	}()

	vmmLogger.Info("Waiting for machine to stop...")

	machine.Wait(context.Background())

	vmmLogger.Info("Machine to stopped. Persisting the file system...")

	ok, org, name, version := tagDecompose(commandConfig.Tag)
	if !ok {
		stopVMM(vmmCtx, machine, vethIfaceName, remoteClient, vmmLogger)
		vmmLogger.Error("Tag could not be decomposed", "tag", commandConfig.Tag)
		return
	}

	fsFileName := filepath.Base(buildRootfs)
	fileSystemTargetDirectory := filepath.Join(machineConfig.MachineRootFSBase, "_builds", org, name, version)
	fileSystemTarget := filepath.Join(fileSystemTargetDirectory, fsFileName)

	if err := os.MkdirAll(filepath.Dir(fileSystemTarget), 0644); err != nil {
		stopVMM(vmmCtx, machine, vethIfaceName, remoteClient, vmmLogger)
		vmmLogger.Error("Failed creating final build target directory", "reason", err)
		return
	}

	if copyErr := copyFile(filepath.Join(jailingFcConfig.JailerChrootDirectory(), "root", fsFileName), fileSystemTarget, 4*1024*1024); copyErr != nil {
		stopVMM(vmmCtx, machine, vethIfaceName, remoteClient, vmmLogger)
		vmmLogger.Error("Failed copying built file", "tag", commandConfig.Tag)
		return
	}

	// write the metadata to a JSON file:
	metadata := map[string]interface{}{
		"labels":         buildContext.Metadata(),
		"ports":          buildContext.ExposedPorts(),
		"created-at-utc": time.Now().UTC().Unix(),
		"build-context": map[string]interface{}{
			"cni-config": cniConfig,
			"config":     &commandConfig,
		},
	}

	metadataFileName := filepath.Join(fileSystemTargetDirectory, "metadata.json")

	metadataJSONBytes, jsonErr := json.MarshalIndent(&metadata, "", "  ")
	if jsonErr != nil {
		stopVMM(vmmCtx, machine, vethIfaceName, remoteClient, vmmLogger)
		vmmLogger.Error("Machine metadata could not be serialized to JSON", "metadata", metadata, "reason", jsonErr)
		return
	}

	if writeErr := ioutil.WriteFile(metadataFileName, metadataJSONBytes, 0644); writeErr != nil {
		stopVMM(vmmCtx, machine, vethIfaceName, remoteClient, vmmLogger)
		vmmLogger.Error("Machine metadata not written to file", "metadata", metadata, "reason", jsonErr)
		return
	}

	vmmLogger.Info("Build completed successfully. Rootfs tagged.", "storage-path", fileSystemTarget)

}

func stopVMM(ctx context.Context, machine *firecracker.Machine, vethIfaceName string, remoteClient remote.ConnectedClient, logger hclog.Logger) stoppedOK {

	if remoteClient != nil {
		logger.Info("Closing remote client...")
		status := remoteClient.Close()
		logger.Info("Remote client closed", "error", status)
	}

	shutdownCtx, cancelFunc := context.WithTimeout(ctx, time.Second*time.Duration(commandConfig.ShutdownGracefulTimeoutSeconds))
	defer cancelFunc()

	logger.Info("Attempting VMM graceful shutdown...")

	chanStopped := make(chan error, 1)
	go func() {
		// Ask the machine to shut down so the file system gets flushed
		// and out changes are written to disk.
		chanStopped <- machine.Shutdown(shutdownCtx)
	}()

	stoppedState := stoppedForcefully

	select {
	case stopErr := <-chanStopped:
		if stopErr != nil {
			logger.Warn("VMM stopped with error but within timeout", "reason", stopErr)
			logger.Warn("VMM stopped forcefully", "error", machine.StopVMM())
		} else {
			logger.Warn("VMM stopped gracefully")
			stoppedState = stoppedGracefully
		}
	case <-shutdownCtx.Done():
		logger.Warn("VMM failed to stop gracefully: timeout reached")
		logger.Warn("VMM stopped forcefully", "error", machine.StopVMM())
	}

	logger.Info("Cleaning up CNI network...")

	cniCleanupErr := cleanupCNINetwork(cniConfig,
		machine.Cfg.NetNS,
		machine.Cfg.VMID,
		machineConfig.MachineCNINetworkName,
		vethIfaceName)

	logger.Info("CNI network cleanup status", "error", cniCleanupErr)

	return stoppedState
}

func runVMM(ctx context.Context, fcConfig firecracker.Config) (*firecracker.Machine, error) {
	vmmLogger := logrus.New()
	machineOpts := []firecracker.Opt{
		firecracker.WithLogger(logrus.NewEntry(vmmLogger)),
	}
	m, err := firecracker.NewMachine(ctx, fcConfig, machineOpts...)
	if err != nil {
		return nil, fmt.Errorf("Failed creating machine: %s", err)
	}
	if err := m.Start(ctx); err != nil {
		return nil, fmt.Errorf("Failed to start machine: %v", err)
	}
	return m, nil
}

func copyFile(source, dest string, bufferSize int) error {
	sourceFile, err := os.Open(source)
	if err != nil {
		return nil
	}
	destFile, err := os.Create(dest)
	if err != nil {
		return nil
	}
	buf := make([]byte, bufferSize)
	for {
		n, err := sourceFile.Read(buf)
		if err != nil && err != io.EOF {
			return err
		}
		if n == 0 {
			break
		}

		if _, err := destFile.Write(buf[:n]); err != nil {
			return err
		}
	}
	return nil
}

func cleanupCNINetwork(cniConfig *configs.CNIConfig, netNs, vmmID, networkName, ifname string) error {
	cniPlugin := libcni.NewCNIConfigWithCacheDir([]string{cniConfig.BinDir}, cniConfig.CacheDir, nil)
	networkConfig, err := libcni.LoadConfList(cniConfig.ConfDir, networkName)
	if err != nil {
		return err
	}
	if err := cniPlugin.DelNetworkList(context.Background(), networkConfig, &libcni.RuntimeConf{
		ContainerID: vmmID, // golang firecracker SDK uses the VMID, if VMID is set
		NetNS:       netNs,
		IfName:      ifname,
	}); err != nil {
		return err
	}
	return nil
}

func getRandomVethName() string {
	return fmt.Sprintf("veth%s", utils.RandStringBytes(11))
}
