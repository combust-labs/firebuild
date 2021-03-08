package strategy

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/combust-labs/firebuild/pkg/utils"
	"github.com/firecracker-microvm/firecracker-go-sdk"
	"github.com/hashicorp/go-hclog"
	"github.com/pkg/errors"

	"golang.org/x/crypto/ssh"
)

// Handler names
const (
	PseudoCloudInitName = "fcinit.PseudoCloudInit"
)

var defaultHosts = map[string]string{
	"127.0.0.1": "localhost",
	"::1":       "localhost ip6-localhost ip6-loopback",
	"fe00::0":   "ip6-localnet",
	"ff00::0":   "ip6-mcastprefix",
	"ff02::1":   "ip6-allnodes",
	"ff02::2":   "ip6-allrouters",
}

// PseudoCloudInitHandlerConfig configures the handler.
type PseudoCloudInitHandlerConfig struct {
	AuthorizedKeysFile string // if empty, /home/{SSHUser}/.ssh/authorized_keys will be assumed
	Chroot             string
	RootfsFileName     string
	SSHUser            string

	Hostname   string
	PublicKeys []ssh.PublicKey
}

// NewPseudoCloudInitHandler returns a firecracker handler which can be used to inject state into
// a virtual machine file system prior to start.
func NewPseudoCloudInitHandler(logger hclog.Logger, config *PseudoCloudInitHandlerConfig) firecracker.Handler {
	return firecracker.Handler{
		Name: PseudoCloudInitName,
		Fn: func(ctx context.Context, m *firecracker.Machine) error {

			// we have to make sure that we have the file for the rootfs under
			// chroot/root/fs-file-name

			logger = logger.Named(PseudoCloudInitName)

			logger.Debug("checking the jailed rootfs file", "path", config.RootfsFileName)

			jailedRootfsFile := filepath.Join(config.Chroot, "root", config.RootfsFileName)

			if _, statErr := checkIfExistsAndIsRegular(jailedRootfsFile); statErr != nil {
				logger.Error("jailed rootfs file requirements failed", "on-disk-path", config.RootfsFileName, "reason", statErr)
				return statErr
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

			impl := &pseudoCloudInitHandlerImplementation{
				mountedFsRoot: tempDir,
				config:        config,
				logger:        logger,
			}

			if err := impl.injectHostname(); err != nil {
				return err
			}
			if err := impl.injectSSHKeys(); err != nil {
				return err
			}

			// all okay, keys written
			return nil
		},
	}
}

type pseudoCloudInitHandlerImplementation struct {
	mountedFsRoot string
	config        *PseudoCloudInitHandlerConfig
	logger        hclog.Logger
}

func (handler *pseudoCloudInitHandlerImplementation) injectHostname() error {

	if len(handler.config.Hostname) == 0 {
		return nil // nothing to do
	}

	etcHostnameFile := filepath.Join(handler.mountedFsRoot, "/etc/hostname")

	sourceStat, err := checkIfExistsAndIsRegular(etcHostnameFile)
	if err != nil {
		handler.logger.Error("hostname file requirements failed", "on-disk-path", etcHostnameFile, "reason", err)
		return err
	}

	handler.logger.Debug("hostname file ok, going to chmod for writing")

	// I need to chmod it such that I can write it:
	if chmodErr := os.Chmod(etcHostnameFile, 0660); chmodErr != nil {
		handler.logger.Error("failed chmod hostname file for writing", "reason", chmodErr)
		return chmodErr
	}

	defer func() {
		// Chmod it to what it was before:
		handler.logger.Debug("resetting mode perimissions for hostname file")
		if chmodErr := os.Chmod(etcHostnameFile, sourceStat.Mode().Perm()); chmodErr != nil {
			handler.logger.Error("failed resetting chmod hostname file AFTER writing", "reason", chmodErr)
		}
	}()

	handler.logger.Debug("opening hostname file for writing", "current-file-size", sourceStat.Size())

	writableFile, fileErr := os.OpenFile(etcHostnameFile, os.O_RDWR, 0660)
	if fileErr != nil {
		return fmt.Errorf("failed opening the hostname '%s' file for writing: %+v", etcHostnameFile, fileErr)
	}
	defer func() {
		handler.logger.Debug("closing hostname file after writing")
		if err := writableFile.Close(); err != nil {
			handler.logger.Error("failed closing hostname file AFTER writing", "reason", err)
		}
	}()

	written, writeErr := writableFile.WriteString(handler.config.Hostname)
	if writeErr != nil {
		handler.logger.Error("failed writing hostname to file", "reason", writeErr)
	}
	if written != len(handler.config.Hostname) {
		handler.logger.Error("hostname file bytes written != hostname length", "written", written, "required", len(handler.config.Hostname))
	}

	return nil
}

func (handler *pseudoCloudInitHandlerImplementation) injectHosts() error {

	etcHostsFile := filepath.Join(handler.mountedFsRoot, "/etc/hosts")

	sourceStat, err := checkIfExistsAndIsRegular(etcHostsFile)
	if err != nil {
		handler.logger.Error("hosts file requirements failed", "on-disk-path", etcHostsFile, "reason", err)
		return err
	}

	handler.logger.Debug("hosts file ok, going to chmod for writing")

	// I need to chmod it such that I can write it:
	if chmodErr := os.Chmod(etcHostsFile, 0660); chmodErr != nil {
		handler.logger.Error("failed chmod hosts file for writing", "reason", chmodErr)
		return chmodErr
	}

	defer func() {
		// Chmod it to what it was before:
		handler.logger.Debug("resetting mode perimissions for hosts file")
		if chmodErr := os.Chmod(etcHostsFile, sourceStat.Mode().Perm()); chmodErr != nil {
			handler.logger.Error("failed resetting chmod hosts file AFTER writing", "reason", chmodErr)
		}
	}()

	handler.logger.Debug("opening hosts file for writing", "current-file-size", sourceStat.Size())

	writableFile, fileErr := os.OpenFile(etcHostsFile, os.O_RDWR, 0660)
	if fileErr != nil {
		return fmt.Errorf("failed opening the hosts '%s' file for writing: %+v", etcHostsFile, fileErr)
	}
	defer func() {
		handler.logger.Debug("closing hosts file after writing")
		if err := writableFile.Close(); err != nil {
			handler.logger.Error("failed closing hosts file AFTER writing", "reason", err)
		}
	}()

	if err := writableFile.Truncate(0); err != nil {
		return errors.Wrap(err, fmt.Sprintf("failed truncating hosts file '%s'", etcHostsFile))
	}

	for k, v := range defaultHosts {
		hostsString := k + "\t" + v + "\n"
		written, writeErr := writableFile.WriteString(hostsString)
		if writeErr != nil {
			handler.logger.Error("failed writing hosts to file", "reason", writeErr)
		}
		if written != len(hostsString) {
			handler.logger.Error("hosts file bytes written != hosts length", "kv", k+"::"+v, "written", written, "required", len(hostsString))
		}
	}

	return nil
}

func (handler *pseudoCloudInitHandlerImplementation) injectSSHKeys() error {

	if len(handler.config.PublicKeys) == 0 {
		return nil // nothing to do
	}

	// what's the authorized keys file?:
	authorizedKeysFile := handler.config.AuthorizedKeysFile
	if authorizedKeysFile == "" {
		authorizedKeysFile = fmt.Sprintf("/home/%s/.ssh/authorized_keys", handler.config.SSHUser)
	}

	// we expect the authorized key file under
	// mount-dir/authorized-keys-file
	authKeysFullPath := filepath.Join(handler.mountedFsRoot, authorizedKeysFile)

	handler.logger.Debug("authorized_keys file to use", "path", authorizedKeysFile, "on-disk-path", authKeysFullPath)
	handler.logger.Debug("checking the authorized_keys file")

	sourceStat, err := checkIfExistsAndIsRegular(authKeysFullPath)
	if err != nil {
		handler.logger.Error("authorized_keys file requirements failed", "on-disk-path", authKeysFullPath, "reason", err)
		return err
	}

	handler.logger.Debug("authorized_keys file ok, going to chmod for writing")

	// I need to chmod it such that I can write it:
	if chmodErr := os.Chmod(authKeysFullPath, 0660); chmodErr != nil {
		handler.logger.Error("failed chmod authorized_keys file for writing", "reason", chmodErr)
		return chmodErr
	}

	defer func() {
		// Chmod it to what it was before:
		handler.logger.Debug("resetting mode perimissions for authorized_keys file")
		if chmodErr := os.Chmod(authKeysFullPath, sourceStat.Mode().Perm()); chmodErr != nil {
			handler.logger.Error("failed resetting chmod authorized_keys file AFTER writing", "reason", chmodErr)
		}
	}()

	handler.logger.Debug("opening authorized_keys file for writing", "current-file-size", sourceStat.Size())

	writableFile, fileErr := os.OpenFile(authKeysFullPath, os.O_RDWR, 0660)
	if fileErr != nil {
		return fmt.Errorf("failed opening the authorized_keys '%s' file for writing: %+v", authorizedKeysFile, fileErr)
	}
	defer func() {
		handler.logger.Debug("closing authorized_keys file after writing")
		if err := writableFile.Close(); err != nil {
			handler.logger.Error("failed closing authorized_keys file AFTER writing", "reason", err)
		}
	}()

	// make sure we have a new line:
	if sourceStat.Size() > 0 {
		handler.logger.Debug("content found in authorized_keys file, appening new line")
		if _, err := writableFile.Write([]byte("\n")); err != nil {
			handler.logger.Error("failed writing new line authorized_keys file", "reason", err)
			return err
		}
	}
	for _, key := range handler.config.PublicKeys {
		marshaled := utils.MarshalSSHPublicKey(key)
		handler.logger.Debug("writing marshaled key to authorized_keys file", "key", string(marshaled))
		marshaled = append(marshaled, []byte("\n")...)
		written, err := writableFile.Write(marshaled)
		if err != nil {
			handler.logger.Error("failed writing marshaled key to authorized_keys file", "reason", err)
			return err
		}
		expectedToWrite := len(marshaled)
		if written != expectedToWrite {
			handler.logger.Error("written != len", "written", written, "len", expectedToWrite)
		}
	}
	return nil
}

func checkIfExistsAndIsRegular(path string) (fs.FileInfo, error) {
	stat, statErr := os.Stat(path)
	if statErr != nil {
		return nil, errors.Wrap(statErr, "stat failed")
	}

	if !stat.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file", path)
	}
	return stat, nil
}
