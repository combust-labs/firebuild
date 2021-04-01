package remote

import (
	"bufio"
	"context"
	"crypto/rsa"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/combust-labs/firebuild-shared/build/commands"
	"github.com/combust-labs/firebuild-shared/build/resources"
	"github.com/combust-labs/firebuild/pkg/utils"

	"github.com/hashicorp/go-hclog"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

const (
	defaultMaxPacketSize  = 32768
	defaultTimeoutSeconds = 10

	defaultResourceReaderBufferSizeBytes = 1048576
)

// TODO: validate host key in the future

// ConnectConfig contains the data to connect to the remote
type ConnectConfig struct {
	SSHPrivateKey      rsa.PrivateKey
	SSHUsername        string
	IP                 net.IP
	Port               int
	EnableAgentForward bool
	MaxPacketSize      int
	TimeoutSeconds     int
}

// Connect connects to the SSH location location and returns a connected client.
// The connected client contains an SSH and SFTP clients.
// Use SSH client to run remote commands and SFTP client to upload files for the ADD / COPY commands.
func Connect(ctx context.Context, cfg ConnectConfig, logger hclog.Logger) (ConnectedClient, error) {

	hostPort := fmt.Sprintf("%s:%d", cfg.IP.String(), cfg.Port)
	authMethods := []ssh.AuthMethod{}

	signer, err := ssh.ParsePrivateKey(utils.EncodePrivateKeyToPEM(&cfg.SSHPrivateKey))
	if err != nil {
		return nil, fmt.Errorf("Unable to parse private key: %+v", err)
	}
	authMethods = append(authMethods, ssh.PublicKeys(signer))

	if cfg.EnableAgentForward {
		if sshAgent, err := net.Dial("unix", os.Getenv("SSH_AUTH_SOCK")); err == nil {
			authMethods = append(authMethods, ssh.PublicKeysCallback(agent.NewClient(sshAgent).Signers))
		}
	}

	config := ssh.ClientConfig{
		User: cfg.SSHUsername,
		Auth: authMethods,
	}

	config.HostKeyCallback = func(hostname string, remote net.Addr, receivedKey ssh.PublicKey) error {
		/*
			foundKey := false
			for _, retrievedKey := range retrievedKeys {
				if bytes.Compare(retrievedKey.Marshal(), receivedKey.Marshal()) == 0 {
					foundKey = true
					break
				}
			}
			if !foundKey {
				return fmt.Errorf("Failed to verify host key: '%s'", hostKey)
			}
		*/
		// for now, jus taccept whatever key we get
		return nil
	}

	chanConnectedClient := make(chan ConnectedClient, 1)
	chanError := make(chan error, 1)

	go func() {

		waitCtx, waitCloseFunc := context.WithTimeout(ctx, time.Second*time.Duration(func() int {
			if cfg.TimeoutSeconds == 0 {
				return defaultTimeoutSeconds
			}
			return cfg.TimeoutSeconds
		}()))
		defer waitCloseFunc()

		for {

			if err := waitCtx.Err(); err != nil {
				chanError <- err
				return // exit goroutine
			}

			// if can't open SSH, continue in a moment, SSH not available yet
			// of can't authenticate
			sshClient, err := ssh.Dial("tcp", hostPort, &config)
			if err != nil {
				logger.Debug("SSH: not connected yet", "host-port", hostPort, "reason", err)
				<-time.After(time.Second)
				continue
			}

			// if can't open SFTP, fail, we have connected but
			sftpClient, err := sftp.NewClient(sshClient, sftp.MaxPacket(func() int {
				if cfg.MaxPacketSize == 0 {
					return defaultMaxPacketSize
				}
				return cfg.MaxPacketSize
			}()))

			if err != nil {
				logger.Debug("SSH: failed to connect, unable to start the SFTP subsystem", "host-port", hostPort, "reason", err)
				sshClient.Close()
				chanError <- fmt.Errorf("unable to start sftp subsystem: %+v", err)
				return // exit goroutine
			}

			chanConnectedClient <- &defaultConnectedClient{
				connectedUser: cfg.SSHUsername,
				logger:        logger.Named("connected-remote-client"),
				sshClient:     sshClient,
				sftpClient:    sftpClient,
			}
			return

		}
	}()

	select {
	case connectedClient := <-chanConnectedClient:
		close(chanError)
		return connectedClient, nil
	case err := <-chanError:
		close(chanConnectedClient)
		return nil, err
	}

}

// -- Connected client:

// EgressTestTarget represents an IP of FQDN used for the egress test.
type EgressTestTarget = string

// ConnectedClient contains connected SSH and SFTP clients.
type ConnectedClient interface {
	Close() error
	RunCommand(commands.Run) error
	PutResource(resources.ResolvedResource) error
	WaitForEgress(context.Context, EgressTestTarget) error
}

type defaultConnectedClient struct {
	connectedUser string
	logger        hclog.Logger
	sshClient     *ssh.Client
	sftpClient    *sftp.Client
}

func (dcc *defaultConnectedClient) Close() error {
	dcc.sftpClient.Close()
	dcc.sshClient.Close()
	return nil
}

func (dcc *defaultConnectedClient) RunCommand(command commands.Run) error {
	sshSession, err := dcc.sshClient.NewSession()
	if err != nil {
		return err
	}
	defer sshSession.Close()
	if err := sshSession.RequestPty("xterm", 80, 40, ssh.TerminalModes{
		// ssh.ECHO:          0,     // disable echoing
		ssh.TTY_OP_ISPEED: 14400, // input speed = 14.4kbaud
		ssh.TTY_OP_OSPEED: 14400, // output speed = 14.4kbaud
	}); err != nil {
		return err
	}

	stdout, err := sshSession.StdoutPipe()
	if err != nil {
		return fmt.Errorf("Unable to setup stdout for session: %v", err)
	}
	go io.Copy(os.Stdout, stdout)

	stderr, err := sshSession.StderrPipe()
	if err != nil {
		return fmt.Errorf("Unable to setup stderr for session: %v", err)
	}
	go io.Copy(os.Stderr, stderr)

	// We're running the commands by wrapping the command in the shell call so sshSession.Setenv might not do what we intend.
	// Also, we don't really know which shell are we running because it comes as an argument to us
	// so we can't, for example, assume bourne shell -a...
	envString := ""
	for k, v := range command.Env {
		envString = fmt.Sprintf("%s%s=\"%s\"; ", envString, k, v)
	}

	remoteCommand := fmt.Sprintf("sudo mkdir -p %s && sudo %s '%s'\n",
		command.Workdir.Value,
		strings.Join(command.Shell.Commands, " "),
		strings.ReplaceAll(envString+command.Command, "'", "'\\''"))

	dcc.logger.Debug("Running remote command", "command", remoteCommand, "env", command.Env)

	if err := sshSession.Start(remoteCommand); err != nil {
		return fmt.Errorf("Unable to start the SSH session: %v", err)
	}
	defer sshSession.Close()
	if sessionErr := sshSession.Wait(); sessionErr != nil {
		exitErr, ok := sessionErr.(*ssh.ExitError)
		if ok {
			dcc.logger.Error("Remote command finished with error",
				"exit-status", exitErr.ExitStatus(),
				"exit-message", exitErr.Error(),
				"command", remoteCommand)
		}
		return sessionErr
	}
	return nil
}

func (dcc *defaultConnectedClient) PutResource(resource resources.ResolvedResource) error {
	if !resource.IsDir() {
		return dcc.putFileResource(resource)
	}
	return dcc.putDirectoryResource(resource)
}

func (dcc *defaultConnectedClient) putDirectoryResource(resource resources.ResolvedResource) error {

	bootstrapDir := filepath.Join("/tmp", utils.RandStringBytes(32))
	destination := filepath.Join(resource.TargetWorkdir().Value, resource.TargetPath())
	src := resource.ResolvedURIOrPath()
	if !strings.HasSuffix(src, "/") {
		src = src + "/"
	}

	// 1. Create a bootstrap directory:
	runErrBootstrapDir := dcc.RunCommand(commands.RunWithDefaults(fmt.Sprintf("mkdir -p '%s' && chown -R %s '%s'",
		bootstrapDir,
		dcc.connectedUser,
		bootstrapDir)))
	if runErrBootstrapDir != nil {
		return runErrBootstrapDir
	}

	// 2. Upload the directory to the temp location:
	// this bit comes from the Terraform SSH communicator:
	scpFunc := func(w io.Writer, r *bufio.Reader) error {
		uploadEntries := func() error {
			f, err := os.Open(src)
			if err != nil {
				return err
			}
			defer f.Close()

			entries, err := f.Readdir(-1)
			if err != nil {
				return err
			}

			return scpUploadDir(dcc.logger, resource.ResolvedURIOrPath(), entries, w, r)
		}
		if src[len(src)-1] != '/' {
			dcc.logger.Debug("No trailing slash, creating the source directory name")
			return scpUploadDirProtocol(dcc.logger, fs.FileMode(0755), filepath.Base(src), w, r, uploadEntries)
		}
		// Trailing slash, so only upload the contents
		return uploadEntries()
	}

	if scpErr := dcc.scpSession("scp -rvt "+bootstrapDir, scpFunc); scpErr != nil {
		dcc.logger.Error("SCP command failed, directory upload failed",
			"resource", resource.TargetPath(),
			"temp-destination", bootstrapDir,
			"reason", scpErr)
		return scpErr
	}

	// 3. chmod it:
	// can't chmod it using the SFTP client because it may not be the right user...
	modeStrRepr := strconv.FormatUint(uint64(resource.TargetMode()), 8)
	modeStrRepr = modeStrRepr[len(modeStrRepr)-3:]
	// TODO: find out if one needs to chmod -R
	runErrChmod := dcc.RunCommand(commands.RunWithDefaults(fmt.Sprintf("chmod 0%s %s", modeStrRepr, bootstrapDir)))
	if runErrChmod != nil {
		dcc.logger.Warn("WARNING: chmod failed",
			"resource", resource.TargetPath(),
			"temp-destination", bootstrapDir,
			"reason", runErrChmod)
	}

	// 4. chown it
	if resource.TargetUser().Value != "" {
		// can't chown using the SFTP client because it may not be the right user...
		// TODO: find out if one needs to chown -R
		runErrChown := dcc.RunCommand(commands.RunWithDefaults(fmt.Sprintf("chown %s %s", resource.TargetUser().Value, bootstrapDir)))
		if runErrChown != nil {
			dcc.logger.Warn("WARNING: chown failed",
				"resource", resource.TargetPath(),
				"temp-destination", bootstrapDir,
				"reason", runErrChown)
		}
	}

	// 5. Move it to the final destination:
	runErrMove := dcc.RunCommand(commands.RunWithDefaults(fmt.Sprintf("mkdir -p '%s' && mv '%s/*' '%s'", destination, bootstrapDir, destination)))
	if runErrMove != nil {
		return runErrMove
	}

	// 6. Clean up
	runErrCleanup := dcc.RunCommand(commands.RunWithDefaults(fmt.Sprintf("rm -r '%s' && ls -la '%s'", bootstrapDir, destination)))
	if runErrCleanup != nil {
		dcc.logger.Warn("WARNING: failed cleaning up the bootstrap directory",
			"bootstrap-dir", bootstrapDir,
			"reason", runErrCleanup)
	}

	dcc.logger.Debug("Resource moved to final destination",
		"resource", resource.TargetPath(),
		"temp-destination", bootstrapDir,
		"final-destination", destination)

	return nil
}

func (dcc *defaultConnectedClient) putFileResource(resource resources.ResolvedResource) error {

	bootstrapDir := filepath.Join("/tmp", utils.RandStringBytes(32))
	randomFileName := utils.RandStringBytes(32)
	tempDestination := filepath.Join(bootstrapDir, randomFileName)

	destination := filepath.Join(resource.TargetWorkdir().Value, resource.TargetPath())
	targetFileName := filepath.Base(resource.SourcePath())
	if filepath.Base(destination) != targetFileName {
		// ensure that we always have a full target path:
		destination = filepath.Join(destination, targetFileName)
	}
	destinationDirectory := filepath.Dir(destination)

	// 1. Create a bootstrap directory:

	runErrBootstrapDir := dcc.RunCommand(commands.RunWithDefaults(fmt.Sprintf("mkdir -p '%s' && chown -R %s '%s'",
		bootstrapDir,
		dcc.connectedUser,
		bootstrapDir)))
	if runErrBootstrapDir != nil {
		return runErrBootstrapDir
	}

	// 2. Put the file at the temporary destination:

	f, err := dcc.sftpClient.Create(tempDestination)
	if err != nil {
		return err
	}
	defer f.Close()

	resourceReadCloser, err := resource.Contents()
	if err != nil {
		return err
	}
	defer resourceReadCloser.Close()

	resourceBuf := make([]byte, defaultResourceReaderBufferSizeBytes)
	for {
		// read all contents of the resource:
		read, err := resourceReadCloser.Read(resourceBuf)
		if read == 0 && err == io.EOF {
			break // all read
		}
		if err != nil {
			return fmt.Errorf("error reading resource: %+v", err)
		}
		written, err := f.Write(resourceBuf[0:read])
		if err != nil {
			return err
		}
		if written != read {
			return fmt.Errorf("write incomplete for '%s': written bytes: %d, chunk had: %d", resource.TargetPath(), written, read)
		}
	}

	f.Close() // close immediately

	dcc.logger.Debug("Resource uploaded to temporary destination", "resource", resource.TargetPath(), "temp-destination", tempDestination)

	// 3. chmod it:
	// can't chmod it using the SFTP client because it may not be the right user...
	modeStrRepr := strconv.FormatUint(uint64(resource.TargetMode()), 8)
	runErrChmod := dcc.RunCommand(commands.RunWithDefaults(fmt.Sprintf("chmod 0%s \"%s\"", modeStrRepr, tempDestination)))
	if runErrChmod != nil {
		dcc.logger.Warn("WARNING: chmod failed",
			"resource", resource.TargetPath(),
			"temp-destination", tempDestination,
			"reason", runErrChmod)
	}

	// 4. chown it
	if resource.TargetUser().Value != "" {
		// can't chown using the SFTP client because it may not be the right user...
		runErrChown := dcc.RunCommand(commands.RunWithDefaults(fmt.Sprintf("chown %s \"%s\"", resource.TargetUser().Value, tempDestination)))
		if runErrChown != nil {
			dcc.logger.Warn("WARNING: chown failed",
				"resource", resource.TargetPath(),
				"temp-destination", tempDestination,
				"reason", runErrChown)
		}
	}

	// 5. Move it to the final destination:

	runErrMove := dcc.RunCommand(commands.RunWithDefaults(fmt.Sprintf("mkdir -p '%s' && mv \"%s\" \"%s\"", destinationDirectory, tempDestination, destination)))
	if runErrMove != nil {
		return runErrMove
	}

	// 6. Clean up
	runErrCleanup := dcc.RunCommand(commands.RunWithDefaults(fmt.Sprintf("rm -r \"%s\"", bootstrapDir)))
	if runErrCleanup != nil {
		dcc.logger.Warn("WARNING: failed cleaning up the bootstrap directory",
			"bootstrap-dir", bootstrapDir,
			"reason", runErrCleanup)
	}

	dcc.logger.Debug("Resource moved to final destination",
		"resource", resource.TargetPath(),
		"temp-destination", tempDestination,
		"final-destination", destination)

	return nil
}

func (dcc *defaultConnectedClient) WaitForEgress(ctx context.Context, target EgressTestTarget) error {

	chanTimeout := make(chan struct{}, 1)
	chanOK := make(chan struct{}, 1)

	attempt := 1
	benchStart := time.Now()

	go func() {
		for {
			dcc.logger.Info("waiting for egress", "attempt", attempt)
			if ctx.Err() != nil {
				close(chanTimeout)
				return
			}
			if cmdErr := dcc.RunCommand(commands.RunWithDefaults(fmt.Sprintf("ping %s -c 1 -i 0.5", target))); cmdErr != nil {
				dcc.logger.Warn("egress not ready", "attempt", attempt)
				attempt = attempt + 1
				<-time.After(time.Millisecond * 100)
				continue
			}
			dcc.logger.Info("egress ready", "attempt", attempt, "after", time.Now().Sub(benchStart).String())
			close(chanOK)
			return
		}
	}()

	select {
	case <-chanTimeout:
		close(chanOK)
		return fmt.Errorf("waiting for egress timeoud out")
	case <-chanOK:
		close(chanTimeout)
		return nil
	}

}

func fileModeToString(mode fs.FileMode) string {
	return strconv.FormatUint(uint64(mode), 8)
}
