package remote

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/appministry/firebuild/buildcontext/commands"
	"github.com/appministry/firebuild/buildcontext/resources"
	"github.com/appministry/firebuild/buildcontext/utils"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

const (
	defaultMaxPacketSize  = 32768
	defaultTimeoutSeconds = 10
)

// TODO: validate host key in the future

// ConnectConfig contains the data to connect to the remote
type ConnectConfig struct {
	SSKKeyFile          string
	SSHUsername         string
	IP                  net.IP
	Port                int
	DisableAgentForward bool
	MaxPacketSize       int
	TimeoutSeconds      int
}

// Connect connects to the SSH location location and returns a connected client.
// The connected client contains an SSH and SFTP clients.
// Use SSH client to run remote commands and SFTP client to upload files for the ADD / COPY commands.
func Connect(ctx context.Context, cfg ConnectConfig) (ConnectedClient, error) {

	hostPort := fmt.Sprintf("%s:%d", cfg.IP.String(), cfg.Port)
	authMethods := []ssh.AuthMethod{}

	if cfg.SSKKeyFile != "" {
		// TODO: validate that the file exists
		privateKeyBytes, err := ioutil.ReadFile(cfg.SSKKeyFile)
		if err != nil {
			return nil, err
		}
		signer, err := ssh.ParsePrivateKey([]byte(privateKeyBytes))
		if err != nil {
			return nil, fmt.Errorf("Unable to parse private key: %+v", err)
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	}

	if !cfg.DisableAgentForward {
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
				fmt.Println("not connected yet:", fmt.Errorf("unable to connect to [%s]: %+v", hostPort, err))
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
				fmt.Println("failed to connect:", fmt.Errorf("unable to start sftp subsystem: %+v", err))
				sshClient.Close()
				chanError <- fmt.Errorf("unable to start sftp subsystem: %+v", err)
				return // exit goroutine
			}

			chanConnectedClient <- &defaultConnectedClient{
				connectedUser: cfg.SSHUsername,
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

// ConnectedClient contains connected SSH and SFTP clients.
type ConnectedClient interface {
	Close() error
	RunCommand(commands.Run) error
	PutResource(resources.ResolvedResource) error
}

type defaultConnectedClient struct {
	connectedUser string
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

	remoteCommand := fmt.Sprintf("sudo mkdir -p %s && sudo %s '%s'",
		command.Workdir.Value,
		strings.Join(command.Shell.Commands, " "),
		strings.ReplaceAll(command.Command, "'", "''"))
	fmt.Println("Remote client running command: =====> ", remoteCommand)
	return sshSession.Run(remoteCommand)
}

func (dcc *defaultConnectedClient) PutResource(resource resources.ResolvedResource) error {
	if !resource.IsDir() {

		bootstrapDir := filepath.Join("/tmp", utils.RandStringBytes(32))
		randomFileName := utils.RandStringBytes(32)
		tempDestination := filepath.Join(bootstrapDir, randomFileName)

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
		requiredBytes := len(resource.Bytes())
		written, err := f.Write(resource.Bytes())
		if err != nil {
			return err
		}
		if written != len(resource.Bytes()) {
			return fmt.Errorf("write incomplete for '%s': written bytes: %d, file had: %d", resource.TargetPath(), written, requiredBytes)
		}

		f.Close()
		fmt.Println(fmt.Sprintf("Resource '%s' uploaded to temporary destination '%s'.", resource.TargetPath(), tempDestination))

		// 3. chmod it:
		// can't chmod it using the SFTP client because it may not be the right user...
		modeStrRepr := strconv.FormatUint(uint64(resource.TargetMode()), 8)
		runErrChmod := dcc.RunCommand(commands.RunWithDefaults(fmt.Sprintf("chmod 0%s %s", modeStrRepr, tempDestination)))
		if runErrChmod != nil {
			// TODO: log warning - chown failed
			fmt.Println("Chmod failed:", runErrChmod)
		}

		// 4. chown it
		if resource.TargetUser().Value != "" {
			// can't chown using the SFTP client because it may not be the right user...
			runErrChown := dcc.RunCommand(commands.RunWithDefaults(fmt.Sprintf("chown %s %s", resource.TargetUser().Value, tempDestination)))
			if runErrChown != nil {
				// TODO: log warning - chown failed
				fmt.Println("Chown failed:", runErrChown)
			}
		}

		// 5. Move it to the final destination:
		destination := filepath.Dir(filepath.Join(resource.TargetWorkdir().Value, resource.TargetPath()))
		destinationDirectory := filepath.Dir(destination)
		runErrMove := dcc.RunCommand(commands.RunWithDefaults(fmt.Sprintf("mkdir -p '%s' && mv '%s' '%s'", destinationDirectory, tempDestination, destination)))
		if runErrMove != nil {
			return runErrMove
		}

		// 6. Clean up
		runErrCleanup := dcc.RunCommand(commands.RunWithDefaults(fmt.Sprintf("rm -r '%s'", bootstrapDir)))
		if runErrCleanup != nil {
			// TODO: log warning
		}

		fmt.Println(fmt.Sprintf("Resource '%s' moved to final directory location '%s'.", resource.TargetPath(), destination))

		return nil
	}
	return fmt.Errorf("not implemented: directory put")
}
