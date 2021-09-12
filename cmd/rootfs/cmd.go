package rootfs

import (
	"context"
	"fmt"
	"io/fs"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/combust-labs/firebuild-embedded-ca/ca"
	"github.com/combust-labs/firebuild-mmds/mmds"
	"github.com/combust-labs/firebuild-shared/build/commands"
	"github.com/combust-labs/firebuild-shared/build/resources"
	"github.com/combust-labs/firebuild-shared/build/rootfs"
	"github.com/combust-labs/firebuild/configs"
	"github.com/combust-labs/firebuild/pkg/build"
	"github.com/combust-labs/firebuild/pkg/build/reader"
	"github.com/combust-labs/firebuild/pkg/build/stage"
	"github.com/combust-labs/firebuild/pkg/containers"
	"github.com/combust-labs/firebuild/pkg/metadata"
	"github.com/combust-labs/firebuild/pkg/naming"
	"github.com/combust-labs/firebuild/pkg/profiles"
	"github.com/combust-labs/firebuild/pkg/storage"
	"github.com/combust-labs/firebuild/pkg/storage/resolver"
	"github.com/combust-labs/firebuild/pkg/tracing"
	"github.com/opentracing/opentracing-go"

	"github.com/combust-labs/firebuild/pkg/strategy"
	"github.com/combust-labs/firebuild/pkg/strategy/arbitrary"
	"github.com/combust-labs/firebuild/pkg/utils"
	"github.com/combust-labs/firebuild/pkg/vmm"
	firecracker "github.com/firecracker-microvm/firecracker-go-sdk"
	"github.com/spf13/cobra"
)

// Command is the build command declaration.
var Command = &cobra.Command{
	Use:   "rootfs",
	Short: "Build A VMM root file system from a Docker image",
	Run:   run,
	Long:  ``,
}

var (
	cniConfig       = configs.NewCNIConfig()
	commandConfig   = configs.NewRootfsCommandConfig()
	jailingFcConfig = configs.NewJailingFirecrackerConfig()
	logConfig       = configs.NewLogginConfig()
	machineConfig   = configs.NewMachineConfig()
	profilesConfig  = configs.NewProfileCommandConfig()
	runCache        = configs.NewRunCacheConfig()
	tracingConfig   = configs.NewTracingConfig("firebuild-rootfs")

	storageResolver = resolver.NewDefaultResolver()
)

func initFlags() {
	Command.Flags().AddFlagSet(cniConfig.FlagSet())
	Command.Flags().AddFlagSet(commandConfig.FlagSet())
	Command.Flags().AddFlagSet(jailingFcConfig.FlagSet())
	Command.Flags().AddFlagSet(logConfig.FlagSet())
	Command.Flags().AddFlagSet(machineConfig.FlagSet())
	Command.Flags().AddFlagSet(profilesConfig.FlagSet())
	Command.Flags().AddFlagSet(runCache.FlagSet())
	Command.Flags().AddFlagSet(tracingConfig.FlagSet())
	// Storage provider flags:
	resolver.AddStorageFlags(Command.Flags())
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

	rootLogger := logConfig.NewLogger("rootfs")

	if profilesConfig.Profile != "" {
		profile, err := profiles.ReadProfile(profilesConfig.Profile, profilesConfig.ProfileConfDir)
		if err != nil {
			rootLogger.Error("failed resolving profile", "reason", err, "profile", profilesConfig.Profile)
			return 1
		}
		if err := profile.UpdateConfigs(jailingFcConfig, runCache, tracingConfig); err != nil {
			rootLogger.Error("error updating configuration from profile", "reason", err)
			return 1
		}
		storageResolver.
			WithConfigurationOverride(profile.GetMergedStorageConfig()).
			WithTypeOverride(profile.Profile().StorageProvider)
	}

	// tracing:

	rootLogger.Trace("configuring tracing", "enabled", tracingConfig.Enable, "application-name", tracingConfig.ApplicationName)

	tracer, tracerCleanupFunc, tracerErr := tracing.GetTracer(rootLogger.Named("tracer"), tracingConfig)
	if tracerErr != nil {
		rootLogger.Error("failed constructing tracer", "reason", tracerErr)
		return 1
	}

	cleanup.Add(tracerCleanupFunc)

	rootLogger, spanBuild := tracing.ApplyTraceLogDiscovery(rootLogger, tracer.StartSpan("build-rootfs"))
	cleanup.Add(func() {
		spanBuild.Finish()
	})

	storageImpl, resolveErr := storageResolver.GetStorageImpl(rootLogger)
	if resolveErr != nil {
		rootLogger.Error("failed resolving storage provider", "reason", resolveErr)
		spanBuild.SetBaggageItem("error", resolveErr.Error())
		return 1
	}

	validatingConfigs := []configs.ValidatingConfig{
		jailingFcConfig,
		commandConfig,
	}

	for _, validatingConfig := range validatingConfigs {
		if err := validatingConfig.Validate(); err != nil {
			rootLogger.Error("Configuration is invalid", "reason", err)
			spanBuild.SetBaggageItem("error", err.Error())
			return 1
		}
	}

	if commandConfig.Tag == "" {
		rootLogger.Error("--tag is required")
		spanBuild.SetBaggageItem("error", "--tag is required")
		return 1
	}

	if !utils.IsValidTag(commandConfig.Tag) {
		rootLogger.Error("--tag value is invalid", "tag", commandConfig.Tag)
		spanBuild.SetBaggageItem("error", fmt.Errorf("--tag value is invalid: '%s'", commandConfig.Tag).Error())
		return 1
	}

	spanTempDir := tracer.StartSpan("rootfs-temp-dir", opentracing.ChildOf(spanBuild.Context()))

	// create cache directory:
	cacheDirectory := filepath.Join(runCache.LocationBuilds(), jailingFcConfig.VMMID())
	if err := os.MkdirAll(cacheDirectory, 0755); err != nil {
		rootLogger.Error("failed creating build VMM cache directory", "reason", err)
		spanTempDir.SetBaggageItem("error", err.Error())
		spanTempDir.Finish()
		return 1
	}

	cleanup.Add(func() {
		span := tracer.StartSpan("rootfs-temp-cleanup", opentracing.ChildOf(spanTempDir.Context()))
		rootLogger.Info("cleaning up temp build directory")
		if err := os.RemoveAll(cacheDirectory); err != nil {
			rootLogger.Info("temp build directory removal status", "error", err)
			span.SetBaggageItem("error", err.Error())
		}
		span.Finish()
	})

	spanTempDir.Finish()

	// -- Command specific:

	if commandConfig.DockerImage != "" {
		// prepare the build context based on the Docker image provided:
		dockerClient, err := containers.GetDefaultClient()
		if err != nil {
			rootLogger.Error("failed fetching Docker client for image pull", "reason", err)
			return 1
		}
		if err := containers.ImagePull(context.Background(), dockerClient, rootLogger, commandConfig.DockerImage); err != nil {
			rootLogger.Error("failed pulling Docker image", "image", commandConfig.DockerImage, "reason", err)
			return 1
		}

		imageMetadata, readErr := containers.ReadImageConfig(context.Background(), dockerClient, rootLogger, commandConfig.DockerImage)
		if readErr != nil {
			rootLogger.Error("failed reading Docker image config", "image", commandConfig.DockerImage, "reason", readErr)
			return 1
		}

		dockerfileLines := containers.HistoryToDockerfile(imageMetadata.Config.History, commandConfig.DockerImageBase)
		dockerfile := filepath.Join(cacheDirectory, "Dockerfile")

		if err := ioutil.WriteFile(dockerfile, []byte(strings.Join(dockerfileLines, "\n")), fs.ModePerm); err != nil {
			rootLogger.Error("failed writing Dockerfile for Docker image in build cache", "image", commandConfig.DockerImage, "reason", err)
			return 1
		}

		readResult, err := reader.ReadFromString(dockerfile, cacheDirectory)
		if err != nil {
			rootLogger.Error("failed parsing Dockerfile for Docker image from build cache", "image", commandConfig.DockerImage, "reason", err)
			return 1
		}

		exportResources := []*containers.ImageResourceExportCommand{}
		for _, cmd := range readResult.Commands() {
			imageExportResource, err := containers.ImageResourceExportFromCommand(cmd)
			if err != nil {
				continue
			}
			exportResources = append(exportResources, imageExportResource)
		}

		_, exportErr := containers.ImageExportResources(context.Background(),
			dockerClient,
			rootLogger,
			cacheDirectory,
			exportResources, commandConfig.DockerImage)
		if exportErr != nil {
			rootLogger.Error("failed exporting resources for Docker image", "image", commandConfig.DockerImage, "reason", exportErr)
			return 1
		}

		commandConfig.Dockerfile = dockerfile
		commandConfig.DockerfileStage = ""

	}

	spanParseDockerfile := tracer.StartSpan("rootfs-parse-dockerfile", opentracing.ChildOf(spanTempDir.Context()))

	readResults, err := reader.ReadFromString(commandConfig.Dockerfile, cacheDirectory)
	if err != nil {
		rootLogger.Error("failed parsing Dockerfile", "reason", err)
		spanParseDockerfile.SetBaggageItem("error", err.Error())
		spanParseDockerfile.Finish()
		return 1
	}

	spanParseDockerfile.Finish()

	spanReadStages := tracer.StartSpan("rootfs-read-stages", opentracing.ChildOf(spanParseDockerfile.Context()))

	scs, errs := stage.ReadStages(readResults.Commands())
	for _, err := range errs {
		rootLogger.Warn("stages read contained an error", "reason", err)
	}

	stageToBuild, stageResolveError := func() (stage.Stage, error) {
		if commandConfig.DockerfileStage == "" {
			unnamed := scs.Unnamed()
			if len(unnamed) != 1 {
				return nil, fmt.Errorf("unnamed stages count must be 1 when no build stage is specified")
			}
			if !unnamed[0].IsValid() {
				return nil, fmt.Errorf("main build stage invalid: no base to build from")
			}
			return unnamed[0], nil
		}
		namedStage := scs.NamedStage(commandConfig.DockerfileStage)
		if namedStage == nil {
			return nil, fmt.Errorf("no stage named %q", commandConfig.DockerfileStage)
		}
		return namedStage, nil
	}()

	if stageResolveError != nil {
		rootLogger.Error(stageResolveError.Error())
		spanReadStages.SetBaggageItem("error", stageResolveError.Error())
		spanReadStages.Finish()
		return 1
	}

	spanReadStages.Finish()

	spanBuildContext := tracer.StartSpan("rootfs-build-context", opentracing.ChildOf(spanReadStages.Context()))

	// The first thing to do is to resolve the Dockerfile:
	contextBuilder := build.NewDefaultBuild()
	if err := contextBuilder.AddInstructions(stageToBuild.Commands()...); err != nil {
		rootLogger.Error("commands could not be processed", "reason", err)
		spanBuildContext.SetBaggageItem("error", err.Error())
		spanBuildContext.Finish()
		return 1
	}

	structuredFrom := contextBuilder.From().ToStructuredFrom()

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
					spanBuildContext.SetBaggageItem("error", "depends on non-existing stage")
					spanBuildContext.Finish()
					return 1
				}
				spanDependencyBuild := tracer.StartSpan("rootfs-build-dependency", opentracing.ChildOf(spanBuildContext.Context()))
				dependencyBuilder := build.NewDefaultDependencyBuild(dependencyStage, cacheDirectory, filepath.Join(cacheDirectory, "sources"))
				resolvedResources, buildError := dependencyBuilder.Build(requiredCopies)
				if buildError != nil {
					rootLogger.Error("failed building stage dependency", "stage", stage.Name(), "dependency", dependency, "reason", buildError)
					spanDependencyBuild.SetBaggageItem("error", buildError.Error())
					spanDependencyBuild.Finish()
					spanBuildContext.Finish()
					return 1
				}
				dependencyResources[dependency] = resolvedResources
				spanDependencyBuild.Finish()
			}
		}
	}

	spanBuildContext.Finish()

	// -- Command specific // END

	spanResolveKernel := tracer.StartSpan("rootfs-resolve-kernel", opentracing.ChildOf(spanBuildContext.Context()))

	// resolve kernel:
	resolvedKernel, kernelResolveErr := storageImpl.FetchKernel(&storage.KernelLookup{
		ID: machineConfig.VMLinuxID,
	})
	if kernelResolveErr != nil {
		rootLogger.Error("failed resolving kernel", "reason", kernelResolveErr)
		spanResolveKernel.SetBaggageItem("error", kernelResolveErr.Error())
		spanResolveKernel.Finish()
		return 1
	}

	rootLogger.Info("kernel resolved", "host-path", resolvedKernel.HostPath())

	spanResolveKernel.Finish()

	spanResolveRootfs := tracer.StartSpan("rootfs-resolve-rootfs", opentracing.ChildOf(spanResolveKernel.Context()))

	// resolve rootfs:
	resolvedRootfs, rootfsResolveErr := storageImpl.FetchRootfs(&storage.RootfsLookup{
		Org:     structuredFrom.Org(),
		Image:   structuredFrom.Image(),
		Version: structuredFrom.Version(),
	})
	if rootfsResolveErr != nil {
		rootLogger.Error("failed resolving rootfs", "reason", rootfsResolveErr)
		spanResolveRootfs.SetBaggageItem("error", rootfsResolveErr.Error())
		spanResolveRootfs.Finish()
		return 1
	}

	rootLogger.Info("rootfs resolved", "host-path", resolvedRootfs.HostPath())

	spanResolveRootfs.Finish()

	spanRootfsCopy := tracer.StartSpan("rootfs-copy", opentracing.ChildOf(spanResolveRootfs.Context()))

	// we do need to copy the rootfs file to a temp directory
	// because the jailer directory indeed links to the target rootfs
	// and changes are persisted
	buildRootfs := filepath.Join(cacheDirectory, naming.RootfsFileName)
	if err := utils.CopyFile(resolvedRootfs.HostPath(), buildRootfs, utils.RootFSCopyBufferSize); err != nil {
		rootLogger.Error("failed copying requested rootfs to temp build location",
			"source", resolvedRootfs.HostPath(),
			"target", buildRootfs,
			"reason", err)
		spanRootfsCopy.SetBaggageItem("error", err.Error())
		spanRootfsCopy.Finish()
		return 1
	}

	spanRootfsCopy.Finish()

	// don't use resolvedRootfs.HostPath() below this point:
	machineConfig.
		WithKernelOverride(resolvedKernel.HostPath()).
		WithRootfsOverride(buildRootfs)

	// gather the running vmm metadata:
	runMetadata := &metadata.MDRun{
		Type: metadata.MetadataTypeRun,
	}

	// --
	// Prepare build context and start the build time server:
	// --

	postBuildCommands := []commands.Run{}
	for _, cmd := range commandConfig.PostBuildCommands {
		postBuildCommands = append(postBuildCommands, commands.RunWithDefaults(cmd))
	}
	preBuildCommands := []commands.Run{}
	for _, cmd := range commandConfig.PreBuildCommands {
		preBuildCommands = append(preBuildCommands, commands.RunWithDefaults(cmd))
	}

	spanWorkContext := tracer.StartSpan("rootfs-build-exec", opentracing.ChildOf(spanRootfsCopy.Context()))

	executionCtx, buildErr := contextBuilder.
		WithLogger(rootLogger.Named("builder")).
		WithPostBuildCommands(postBuildCommands...).
		WithPreBuildCommands(preBuildCommands...).
		CreateContext(dependencyResources)

	if buildErr != nil {
		rootLogger.Error("failed creating bootstrap work context", "reason", buildErr)
		spanWorkContext.SetBaggageItem("error", buildErr.Error())
		spanWorkContext.Finish()
		return 1
	}

	spanWorkContext.Finish()

	spanEmbeddedCA := tracer.StartSpan("embedded-ca-setup", opentracing.ChildOf(spanWorkContext.Context()))

	embeddedCAConfig := &ca.EmbeddedCAConfig{
		Addresses:     []string{jailingFcConfig.VMMID()},
		CertsValidFor: commandConfig.BootstrapCertsValidity,
		KeySize:       commandConfig.BootstrapCertsKeySize,
	}

	embeddedCA, caSetupErr := ca.NewDefaultEmbeddedCAWithLogger(embeddedCAConfig, rootLogger.Named("embedded-ca"))
	if caSetupErr != nil {
		rootLogger.Error("failed setting up VM build embedded CA", "reason", caSetupErr)
		spanEmbeddedCA.SetBaggageItem("error", caSetupErr.Error())
		spanEmbeddedCA.Finish()
		return 1
	}

	spanEmbeddedCA.Finish()

	spanServerTLSConfig := tracer.StartSpan("embedded-ca-server-tls", opentracing.ChildOf(spanEmbeddedCA.Context()))

	serverTLSConfig, serverTLSConfigErr := embeddedCA.NewServerCertTLSConfig()
	if serverTLSConfigErr != nil {
		rootLogger.Error("failed creating bootstrap server TLS config", "reason", serverTLSConfigErr)
		spanServerTLSConfig.SetBaggageItem("error", serverTLSConfigErr.Error())
		spanServerTLSConfig.Finish()
		return 1
	}

	spanServerTLSConfig.Finish()

	spanRootfsServerStart := tracer.StartSpan("rootfs-server-start", opentracing.ChildOf(spanServerTLSConfig.Context()))

	interfaceName, interfaceErr := utils.GetConfiguredOrSuitableInterfaceName(commandConfig.BootstrapServerBindInterface)
	if interfaceErr != nil {
		// this error happens only when no interface name was configured:
		rootLogger.Error("failed fetching the interface to bootstrap on, configure the interface via command flags", "reason", interfaceErr)
		spanRootfsServerStart.SetBaggageItem("error", interfaceErr.Error())
		spanRootfsServerStart.Finish()
		return 1
	}

	rootLogger.Info("fetching a suitable IPv4 address to bind the bootstrap on", "interface", interfaceName)

	ifaceIP, ifaceIPErr := utils.GetInterfaceV4Addr(interfaceName)
	if ifaceIPErr != nil {
		rootLogger.Error("failed fetching IP address of the configured interface", "reason", ifaceIPErr)
		spanRootfsServerStart.SetBaggageItem("error", ifaceIPErr.Error())
		spanRootfsServerStart.Finish()
		return 1
	}

	rootLogger.Info("IPv4 address to bind the bootstrap on was found", interfaceName, ifaceIP)

	rootfsServerConfig := &rootfs.GRPCServiceConfig{
		BindHostPort:    fmt.Sprintf("%s:0", ifaceIP),
		TLSConfigServer: serverTLSConfig,
	}

	rootfsServer := rootfs.New(rootfsServerConfig, rootLogger.Named("build-server"))
	rootfsServer.Start(executionCtx)

	select {
	case startErr := <-rootfsServer.FailedNotify():
		rootLogger.Error("build server did not start", "reason", startErr)
		spanRootfsServerStart.SetBaggageItem("error", startErr.Error())
		spanRootfsServerStart.Finish()
		return 1
	case <-rootfsServer.ReadyNotify():
		cleanup.Add(func() {
			rootfsServer.Stop()
		})
		rootLogger.Info("build server started and serving", "host-port", rootfsServerConfig.BindHostPort)
	}

	spanRootfsServerStart.Finish()

	spanClientTLSConfig := tracer.StartSpan("embedded-ca-client-tls", opentracing.ChildOf(spanRootfsServerStart.Context()))

	clientCertData, clientCertErr := embeddedCA.NewClientCert()
	if clientCertErr != nil {
		rootLogger.Error("failed creating client certificate for MMDS bootstrap", "reason", clientCertErr)
		spanClientTLSConfig.SetBaggageItem("error", clientCertErr.Error())
		spanClientTLSConfig.Finish()
		return 1
	}

	spanClientTLSConfig.Finish()

	spanRootfsBuildMetadata := tracer.StartSpan("rootfs-build-metadata", opentracing.ChildOf(spanClientTLSConfig.Context()))

	runMetadata.Configs.Machine = machineConfig
	runMetadata.Configs.CNI = cniConfig
	runMetadata.Bootstrap = &mmds.MMDSBootstrap{
		HostPort:    rootfsServerConfig.BindHostPort,
		CaChain:     strings.Join(embeddedCA.CAPEMChain(), "\n"),
		Certificate: string(clientCertData.CertificatePEM()),
		Key:         string(clientCertData.KeyPEM()),
		ServerName:  jailingFcConfig.VMMID(),
	}
	// provide safe defaults:
	runMetadata.Configs.RunConfig = &configs.RunCommandConfig{
		EnvFiles:      []string{},
		EnvVars:       map[string]string{},
		IdentityFiles: []string{},
		Hostname:      "",
	}
	runMetadata.Rootfs = &metadata.MDRootfs{
		EntrypointInfo: &mmds.MMDSRootfsEntrypointInfo{},
	}

	// --
	// Ready to start the VM and bootstrap:
	// --

	vethIfaceName := naming.GetRandomVethName()

	vmmLogger := rootLogger.With("vmm-id", jailingFcConfig.VMMID(), "veth-name", vethIfaceName)

	vmmLogger.Info("building VMM",
		"dockerfile", commandConfig.Dockerfile,
		"kernel-path", resolvedKernel.HostPath(),
		"source-rootfs", machineConfig.RootfsOverride(),
		"jail", jailingFcConfig.JailerChrootDirectory())

	cleanup.Add(func() {
		span := tracer.StartSpan("rootfs-cleanup-temp", opentracing.ChildOf(spanBuild.Context()))
		vmmLogger.Info("cleaning up jail directory")
		if err := os.RemoveAll(jailingFcConfig.JailerChrootDirectory()); err != nil {
			vmmLogger.Info("jail directory removal status", "error", err)
			span.SetBaggageItem("error", err.Error())
		}
		span.Finish()
	})

	strategy := configs.DefaultFirecrackerStrategy(machineConfig).
		AddRequirements(func() *arbitrary.HandlerPlacement {
			// add this one after the previous one so by he logic,
			// this one will be placed and executed before the first one
			return arbitrary.NewHandlerPlacement(strategy.
				NewMetadataExtractorHandler(rootLogger, runMetadata), firecracker.CreateBootSourceHandlerName)
		})

	spanRootfsBuildMetadata.Finish()

	spanVMMCreate := tracer.StartSpan("rootfs-vmm-create", opentracing.ChildOf(spanRootfsBuildMetadata.Context()))

	vmmProvider := vmm.NewDefaultProvider(cniConfig, jailingFcConfig, machineConfig).
		WithHandlersAdapter(strategy).
		WithVethIfaceName(vethIfaceName)

	vmmCtx, vmmCancel := context.WithCancel(context.Background())
	cleanup.Add(func() {
		vmmCancel()
	})

	spanVMMCreate.Finish()

	spanVMMStart := tracer.StartSpan("rootfs-vmm-start", opentracing.ChildOf(spanVMMCreate.Context()))

	startedMachine, runErr := vmmProvider.Start(vmmCtx)
	if runErr != nil {
		vmmLogger.Error("Firecracker VMM did not start, build failed", "reason", runErr)
		spanVMMStart.SetBaggageItem("error", runErr.Error())
		spanVMMStart.Finish()
		return 1
	}

	spanVMMStart.Finish()

	spanBootstrapping := tracer.StartSpan("rootfs-boostrapping", opentracing.FollowsFrom(spanRootfsServerStart.Context()))

	ipAddress := runMetadata.NetworkInterfaces[0].StaticConfiguration.IPConfiguration.IP
	gateway := runMetadata.NetworkInterfaces[0].StaticConfiguration.IPConfiguration.Gateway

	vmmLogger = vmmLogger.With("ip-address", ipAddress)

	vmmLogger.Info("VMM running, waiting for bootstrap to finish", "gateway", gateway)

	// --
	// Waiting for bootstrap to complete
	// --

	chanAborted := make(chan error, 1)
	chanSucceeded := make(chan struct{}, 1)

	select {
	case <-time.After(commandConfig.BootstrapInitialCommunicationTimeout):
		spanBootstrapping.SetBaggageItem("error", "VM did not communicate within timeout, bootstrap aborted")
		spanBootstrapping.Finish()
		vmmLogger.Error("VM did not communicate within timeout, aborting bootstrap")
		startedMachine.StopAndWait(vmmCtx)
		return 1
	case firstMessage := <-rootfsServer.OnMessage():
		// first message must be the commands fetched control message:
		switch firstMessage.(type) {
		case *rootfs.ControlMsgCommandsRequested:
		default:
			// invalid communication from the client:
			spanBootstrapping.SetBaggageItem("error", "VM not initiated communication with commands request")
			spanBootstrapping.Finish()
			vmmLogger.Error("invalid communication from the client, expected *rootfs.ControlMsgCommandsRequested", "received", fmt.Sprintf("%_T", firstMessage))
			startedMachine.StopAndWait(vmmCtx)
			return 1
		}
	}

	go func() {
		for {
			nextMessage := <-rootfsServer.OnMessage()
			switch tNextMessage := nextMessage.(type) {
			case *rootfs.ClientMsgAborted:
				chanAborted <- tNextMessage.Error
				return
			case *rootfs.ClientMsgSuccess:
				close(chanSucceeded)
				return
			case *rootfs.ClientMsgStderr:
				for _, line := range tNextMessage.Lines {
					fmt.Fprintln(os.Stderr, strings.TrimSpace(line))
				}
			case *rootfs.ClientMsgStdout:
				for _, line := range tNextMessage.Lines {
					fmt.Fprintln(os.Stdout, strings.TrimSpace(line))
				}
			case *rootfs.ControlMsgPingSent:
				rootLogger.Debug("received ping from bootstrap client")
			}
		}
	}()

	select {
	case abortError := <-chanAborted:
		spanBootstrapping.SetBaggageItem("error", abortError.Error())
		spanBootstrapping.Finish()
		vmmLogger.Error("VM aborted bootstrap with error", "reason", abortError)
		startedMachine.StopAndWait(vmmCtx)
		return 1
	case <-chanSucceeded:
		vmmLogger.Info("VM finished bootstrap successfully")
	}

	spanBootstrapping.Finish()

	// --
	// END / Waiting for bootstrap to complete
	// --

	spanStop := tracer.StartSpan("rootfs-vmm-stop", opentracing.FollowsFrom(spanBootstrapping.Context()))

	startedMachine.StopAndWait(vmmCtx)

	spanStop.Finish()

	vmmLogger.Info("Machine is stopped. Persisting the file system...")

	spanPersist := tracer.StartSpan("rootfs-persist", opentracing.ChildOf(spanStop.Context()))

	ok, org, name, version := utils.TagDecompose(commandConfig.Tag)
	if !ok {
		vmmLogger.Error("Tag could not be decomposed", "tag", commandConfig.Tag)
		spanPersist.SetBaggageItem("error", "--tag could not be decomposed")
		spanPersist.Finish()
		return 1
	}

	buildEntrypointInfo := contextBuilder.EntrypointInfo()

	fsFileName := filepath.Base(machineConfig.RootfsOverride())
	createdRootfsFile := filepath.Join(jailingFcConfig.JailerChrootDirectory(), "root", fsFileName)
	storeResult, storeErr := storageImpl.StoreRootfsFile(&storage.RootfsStore{
		LocalPath: createdRootfsFile,
		Metadata: metadata.MDRootfs{
			BuildConfig: metadata.MDRootfsConfig{
				BuildArgs:         commandConfig.BuildArgs,
				Dockerfile:        commandConfig.Dockerfile,
				DockerImage:       commandConfig.DockerImage,
				DockerImageBase:   commandConfig.DockerImageBase,
				PreBuildCommands:  commandConfig.PreBuildCommands,
				PostBuildCommands: commandConfig.PostBuildCommands,
			},
			CreatedAtUTC: time.Now().UTC().Unix(),
			EntrypointInfo: &mmds.MMDSRootfsEntrypointInfo{
				Cmd:        buildEntrypointInfo.Cmd.Values,
				Entrypoint: buildEntrypointInfo.Entrypoint.Values,
				Env:        buildEntrypointInfo.Entrypoint.Env,
				Shell:      buildEntrypointInfo.Entrypoint.Shell.Commands,
				User:       buildEntrypointInfo.Entrypoint.User.Value,
				Workdir:    buildEntrypointInfo.Entrypoint.Workdir.Value,
			},
			Image: metadata.MDImage{
				Org:     org,
				Image:   name,
				Version: version,
			},
			Labels:  contextBuilder.Metadata(),
			Parent:  resolvedRootfs.Metadata(),
			Ports:   contextBuilder.ExposedPorts(),
			Tag:     commandConfig.Tag,
			Type:    metadata.MetadataTypeRootfs,
			Volumes: contextBuilder.Volumes(),
		},
		Org:     org,
		Image:   name,
		Version: version,
	})

	if storeErr != nil {
		vmmLogger.Error("failed storing built rootfs", "reason", storeErr)
		spanPersist.SetBaggageItem("error", storeErr.Error())
		spanPersist.Finish()
		return 1
	}

	spanPersist.Finish()

	vmmLogger.Info("Build completed successfully. Rootfs tagged.", "output", storeResult)

	return 0

}
