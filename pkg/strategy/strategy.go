package strategy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/combust-labs/firebuild/pkg/utils"
	"github.com/firecracker-microvm/firecracker-go-sdk"
	"github.com/hashicorp/go-hclog"

	"golang.org/x/crypto/ssh"
)

// Handler names
const (
	SSHKeyInjectingHandlerName = "fcinit.SSHKeyInjectingStrategy"
)

// SSHKeyInjectingHandlerConfig configures the SSH key injecting handler.
type SSHKeyInjectingHandlerConfig struct {
	AuthorizedKeysFile string // if empty, /home/{SSHUser}/.ssh/authorized_keys will be assumed
	Chroot             string
	RootfsFileName     string
	SSHUser            string
	PublicKeys         []ssh.PublicKey
}

// NewSSHKeyInjectingHandler returns a firecracker handler which can be used to inject ssh authorized keys into
// a virtual machine file system prior to start.
func NewSSHKeyInjectingHandler(logger hclog.Logger, config *SSHKeyInjectingHandlerConfig) firecracker.Handler {
	return firecracker.Handler{
		Name: SSHKeyInjectingHandlerName,
		Fn: func(ctx context.Context, m *firecracker.Machine) error {

			if len(config.PublicKeys) == 0 {
				return nil
			}

			// we have to make sure that we have the file for the rootfs under
			// chroot/root/fs-file-name

			logger = logger.Named(SSHKeyInjectingHandlerName)

			logger.Debug("checking the jailed rootfs file", "path", config.RootfsFileName)

			jailedRootfsFile := filepath.Join(config.Chroot, "root", config.RootfsFileName)
			stat, statErr := os.Stat(jailedRootfsFile)
			if statErr != nil {
				logger.Error("jailed rootfs file check failed", "path", config.RootfsFileName, "reason", statErr)
				return statErr
			}
			// it must be a regular file:
			if !stat.Mode().IsRegular() {
				logger.Error("jailed rootfs file mist be a regular file", "path", config.RootfsFileName)
				return fmt.Errorf("jailed '%s' must be a regular file", jailedRootfsFile)
			}

			logger.Debug("creating temp directory to mount jailed rootfs file")

			// if it is, we need a temp directory where we can mount the file
			tempDir, tempDirErr := os.MkdirTemp("", "")
			if tempDirErr != nil {
				logger.Error("failed creating temp dir for jailed rootfs mount", "reason", tempDirErr)
				return fmt.Errorf("failed creating temp dir for jailed rootfs mount: %+v", tempDirErr)
			}

			defer func() {
				logger.Debug("cleaning up temp directory")
				if err := os.RemoveAll(tempDir); err != nil {
					logger.Error("failed cleaning up temp directory", "reason", err)
				}
			}()

			logger.Debug("mouting jailed rootfs file")

			// now we can mount the rootfs file:
			if mountErr := utils.Mount(jailedRootfsFile, tempDir); mountErr != nil {
				logger.Error("failed mounting jailed rootfs under temp directory", "reason", mountErr)
				return mountErr
			}

			// we've mounted, defer the umount
			defer func() {
				logger.Debug("unmounting jailed rootfs from temp directory")
				if umountErr := utils.Umount(tempDir); umountErr != nil {
					logger.Error("failed unmounting the jailed rootfs", "location", tempDir, "reason:", umountErr)
				}
			}()

			// what's the authorized keys file?:
			authorizedKeysFile := config.AuthorizedKeysFile
			if authorizedKeysFile == "" {
				authorizedKeysFile = fmt.Sprintf("/home/%s/.ssh/authorized_keys", config.SSHUser)
			}

			// we expect the authorized key file under
			// mount-dir/authorized-keys-file
			authKeysFullPath := filepath.Join(tempDir, authorizedKeysFile)

			logger.Debug("authorized_keys file to use", "path", authorizedKeysFile, "on-disk-path", authKeysFullPath)
			logger.Debug("checking the authorized_keys file")

			authKeysStat, authKeysStatErr := os.Stat(authKeysFullPath)
			if authKeysStatErr != nil {
				logger.Error("authorized_keys file check failed", "on-disk-path", authKeysFullPath, "reason", authKeysStatErr)
				return authKeysStatErr
			}

			if !authKeysStat.Mode().IsRegular() {
				logger.Error("authorized_keys file must be a regular file")
				return fmt.Errorf("authorized_keys '%s' must be a regular file", authorizedKeysFile)
			}

			logger.Debug("authorized_keys file ok, going to chmod for writing")

			// I need to chmod it such that I can write it:
			if chmodErr := os.Chmod(authKeysFullPath, 0660); chmodErr != nil {
				logger.Error("failed chmod authorized_keys file for writing", "reason", chmodErr)
				return chmodErr
			}

			defer func() {
				// Chmod it to what it was before:
				logger.Debug("resetting mode perimissions for authorized_keys file")
				if chmodErr := os.Chmod(authKeysFullPath, authKeysStat.Mode().Perm()); chmodErr != nil {
					logger.Error("failed resetting chmod authorized_keys file AFTER writing", "reason", chmodErr)
				}
			}()

			logger.Debug("opening authorized_keys file for writing", "current-file-size", authKeysStat.Size())

			writableFile, fileErr := os.OpenFile(authKeysFullPath, os.O_RDWR, 0660)
			if fileErr != nil {
				return fmt.Errorf("failed opening the authorized_keys '%s' file for writing: %+v", authorizedKeysFile, fileErr)
			}
			defer func() {
				logger.Debug("closing authorized_keys file after writing")
				if err := writableFile.Close(); err != nil {
					logger.Error("failed closing authorized_keys file AFTER writing", "reason", err)
				}
			}()

			// make sure we have a new line:
			if authKeysStat.Size() > 0 {
				logger.Debug("content found in authorized_keys file, appening new line")
				if _, err := writableFile.Write([]byte("\n")); err != nil {
					logger.Error("failed writing new line authorized_keys file", "reason", err)
					return err
				}
			}
			for _, key := range config.PublicKeys {
				marshaled := utils.MarshalSSHPublicKey(key)
				logger.Debug("writing marshaled key to authorized_keys file", "key", string(marshaled))
				marshaled = append(marshaled, []byte("\n")...)
				written, err := writableFile.Write(marshaled)
				if err != nil {
					logger.Error("failed writing marshaled key to authorized_keys file", "reason", err)
					return err
				}
				expectedToWrite := len(marshaled)
				if written != expectedToWrite {
					logger.Error("written != len", "written", written, "len", expectedToWrite)
				}
			}

			// all okay, keys written
			return nil
		},
	}
}

/*
// SSHKeyInjectingStrategy injects the SSH public keys into a file system
// before Jailer takes over.
type SSHKeyInjectingStrategy struct {
	config           *SSHKeyInjectingHandlerConfig
	handlerProviders []func() HandlerWithRequirement
	logger           hclog.Logger
}

// NewSSHKeyInjectingStrategy returns a new NaivceChrootStrategy
func NewSSHKeyInjectingStrategy(logger hclog.Logger, config *SSHKeyInjectingHandlerConfig, handlerProvider ...func() HandlerWithRequirement) SSHKeyInjectingStrategy {
	return SSHKeyInjectingStrategy{
		config:           config,
		handlerProviders: handlerProvider,
		logger:           logger,
	}
}

// AdaptHandlers will inject the LinkFilesHandler into the handler list.
func (s SSHKeyInjectingStrategy) AdaptHandlers(handlers *firecracker.Handlers) error {
	for _, handlerProvider := range s.handlerProviders {
		requirement := handlerProvider()
		if !handlers.FcInit.Has(requirement.AppendAfter) {
			return ErrRequiredHandlerMissing
		}
		handlers.FcInit = handlers.FcInit.AppendAfter(
			requirement.AppendAfter,
			requirement.Handler,
		)
	}

	if !handlers.FcInit.Has(firecracker.CreateBootSourceHandlerName) {
		return ErrRequiredHandlerMissing
	}

	// and we depend on:
	handlers.FcInit = handlers.FcInit.AppendAfter(
		firecracker.CreateBootSourceHandlerName,
		NewSSHKeyInjectingHandler(s.logger, s.config),
	)

	return nil
}
*/
