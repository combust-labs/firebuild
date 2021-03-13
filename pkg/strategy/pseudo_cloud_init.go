package strategy

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/combust-labs/firebuild/pkg/metadata"
	"github.com/combust-labs/firebuild/pkg/naming"
	"github.com/combust-labs/firebuild/pkg/utils"
	"github.com/firecracker-microvm/firecracker-go-sdk"
	"github.com/hashicorp/go-hclog"
	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"

	"github.com/hashicorp/go-multierror"

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

	Metadata *metadata.MDRun

	Environment map[string]string
	Hostname    string
	PublicKeys  []ssh.PublicKey

	Tracer      opentracing.Tracer
	SpanContext opentracing.SpanContext
}

// NewPseudoCloudInitHandler returns a firecracker handler which can be used to inject state into
// a virtual machine file system prior to start.
func NewPseudoCloudInitHandler(logger hclog.Logger, config *PseudoCloudInitHandlerConfig) firecracker.Handler {
	return firecracker.Handler{
		Name: PseudoCloudInitName,
		Fn: func(ctx context.Context, m *firecracker.Machine) error {

			var cniIface *firecracker.NetworkInterface
			for idx, iface := range m.Cfg.NetworkInterfaces {
				if iface.CNIConfiguration != nil {
					cniIface = &m.Cfg.NetworkInterfaces[idx]
					break
				}
			}

			logger = logger.Named(PseudoCloudInitName)

			logger.Debug("checking the jailed rootfs file", "path", config.RootfsFileName, "hostname", config.Hostname)

			spanPseudoCloudInit := config.Tracer.StartSpan("strategy-pci", opentracing.ChildOf(config.SpanContext))
			defer spanPseudoCloudInit.Finish()

			jailedRootfsFile := filepath.Join(config.Chroot, "root", config.RootfsFileName)

			if _, statErr := utils.CheckIfExistsAndIsRegular(jailedRootfsFile); statErr != nil {
				logger.Error("jailed rootfs file requirements failed", "on-disk-path", config.RootfsFileName, "reason", statErr)
				return statErr
			}

			logger.Debug("creating temp directory to mount jailed rootfs file")

			spanPseudoCloudInitTempDir := config.Tracer.StartSpan("strategy-pci-temp-dir", opentracing.ChildOf(spanPseudoCloudInit.Context()))

			// if it is, we need a temp directory where we can mount the file
			tempDir, tempDirErr := os.MkdirTemp("", "")
			if tempDirErr != nil {
				logger.Error("failed creating temp dir for jailed rootfs mount", "reason", tempDirErr)
				spanPseudoCloudInitTempDir.Finish()
				return fmt.Errorf("failed creating temp dir for jailed rootfs mount: %+v", tempDirErr)
			}

			defer func() {
				logger.Debug("cleaning up temp directory")
				if err := os.RemoveAll(tempDir); err != nil {
					logger.Error("failed cleaning up temp directory", "reason", err)
				}
			}()

			spanPseudoCloudInitTempDir.Finish()

			logger.Debug("mouting jailed rootfs file")

			spanPseudoCloudInitMountDir := config.Tracer.StartSpan("strategy-pci-mount", opentracing.ChildOf(spanPseudoCloudInitTempDir.Context()))

			// now we can mount the rootfs file:
			if mountErr := utils.Mount(jailedRootfsFile, tempDir); mountErr != nil {
				logger.Error("failed mounting jailed rootfs under temp directory", "reason", mountErr)
				spanPseudoCloudInitMountDir.Finish()
				return mountErr
			}

			// we've mounted, defer the umount
			defer func() {
				logger.Debug("unmounting jailed rootfs from temp directory")
				if umountErr := utils.Umount(tempDir); umountErr != nil {
					logger.Error("failed unmounting the jailed rootfs", "location", tempDir, "reason:", umountErr)
				}
			}()

			spanPseudoCloudInitMountDir.Finish()

			impl := &pseudoCloudInitHandlerImplementation{
				mountedFsRoot: tempDir,
				config:        config,
				logger:        logger,
			}

			ops := []func() error{
				func() error {
					span := config.Tracer.StartSpan("strategy-pci-inject-env", opentracing.ChildOf(spanPseudoCloudInitMountDir.Context()))
					defer span.Finish()
					return impl.injectEnvironment()
				},
				func() error {
					span := config.Tracer.StartSpan("strategy-pci-inject-hostname", opentracing.ChildOf(spanPseudoCloudInitMountDir.Context()))
					defer span.Finish()
					return impl.injectHostname()
				},
				func() error {
					span := config.Tracer.StartSpan("strategy-pci-inject-hosts", opentracing.ChildOf(spanPseudoCloudInitMountDir.Context()))
					defer span.Finish()
					return impl.injectHosts(cniIface)
				},
				func() error {
					span := config.Tracer.StartSpan("strategy-pci-inject-metadata", opentracing.ChildOf(spanPseudoCloudInitMountDir.Context()))
					defer span.Finish()
					return impl.injectMetadata()
				},
				func() error {
					span := config.Tracer.StartSpan("strategy-pci-inject-netinfo", opentracing.ChildOf(spanPseudoCloudInitMountDir.Context()))
					defer span.Finish()
					return impl.injectNetInfo(cniIface)
				},
				func() error {
					span := config.Tracer.StartSpan("strategy-pci-inject-sshkeys", opentracing.ChildOf(spanPseudoCloudInitMountDir.Context()))
					defer span.Finish()
					return impl.injectSSHKeys()
				},
			}

			chanErrs := make(chan error, len(ops))
			receivedErrs := []error{}
			numOps := len(ops)

			for _, op := range ops {
				go func(f func() error) {
					chanErrs <- f()
				}(op)
			}

			for {
				receivedErrs = append(receivedErrs, <-chanErrs)
				if len(receivedErrs) == numOps {
					break
				}
			}

			var resultErr error

			for _, err := range receivedErrs {
				if err != nil {
					resultErr = multierror.Append(resultErr, err)
				}
			}

			// all okay, keys written
			return resultErr
		},
	}
}

type pseudoCloudInitHandlerImplementation struct {
	mountedFsRoot string
	config        *PseudoCloudInitHandlerConfig
	logger        hclog.Logger
}

func (handler *pseudoCloudInitHandlerImplementation) injectEnvironment() error {
	if handler.config.Environment == nil {
		return nil // nothing to do
	}
	if len(handler.config.Environment) == 0 {
		return nil // nothing to do
	}
	envFile := filepath.Join(handler.mountedFsRoot, naming.RunEnvVarsFile)
	// make sure a parent directory exists:
	dirExists, err := utils.PathExists(filepath.Dir(envFile))
	if err != nil {
		handler.logger.Error("failed checking if env file parent directory exists", "reason", err)
		return err
	}
	if !dirExists {
		handler.logger.Debug("creating env file parent directory", "env-file", envFile)
		if err := os.Mkdir(filepath.Dir(envFile), 0755); err != nil { // the default permission for this directory
			return errors.Wrap(err, "failed creating parent env directory")
		}
	}

	handler.logger.Debug("writing env file", "parent-existed", dirExists)

	writableFile, openErr := os.OpenFile(envFile, os.O_CREATE|os.O_RDWR, 0755)
	if openErr != nil {
		handler.logger.Error("failed opening env file for writing", "reason", openErr)
		return errors.Wrap(openErr, "failed opening env file for writing")
	}
	defer writableFile.Close()

	for k, v := range handler.config.Environment {
		line := fmt.Sprintf("export %s=\"%s\"\n", k, strings.ReplaceAll(v, "\"", "\\\""))
		written, writeErr := writableFile.WriteString(line)
		if err != nil {
			return errors.Wrap(writeErr, "env file write failed: see error")
		}
		if written != len(line) {
			handler.logger.Error("env file bytes written != line length", "kv", k+"::"+v, "written", written, "required", len(line))
			return errors.New("env file write failed: written != length")
		}
	}

	return nil
}

func (handler *pseudoCloudInitHandlerImplementation) injectHostname() error {

	if len(handler.config.Hostname) == 0 {
		return nil // nothing to do
	}

	etcHostnameFile := filepath.Join(handler.mountedFsRoot, "/etc/hostname")

	sourceStat, err := utils.CheckIfExistsAndIsRegular(etcHostnameFile)
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
		return errors.Wrap(writeErr, "hostname file write failed: see error")
	}
	if written != len(handler.config.Hostname) {
		handler.logger.Error("hostname file bytes written != hostname length", "written", written, "required", len(handler.config.Hostname))
		return errors.New("hostname file write failed: written != length")
	}

	return nil
}

func (handler *pseudoCloudInitHandlerImplementation) injectHosts(cniIface *firecracker.NetworkInterface) error {

	etcHostsFile := filepath.Join(handler.mountedFsRoot, "/etc/hosts")

	hosts := map[string]string{}
	for k, v := range defaultHosts {
		if k == "127.0.0.1" || k == "::1" {
			if cniIface == nil && handler.config.Hostname != "" {
				// if there is no CNI interface and hostname is given,
				// make 127.0.0.1 reply to the hostname
				v = v + " " + handler.config.Hostname
			}
		}
		hosts[k] = v
	}
	if cniIface != nil && handler.config.Hostname != "" {
		// if there is a CNI interface and we have a hostname, make the hostname reply to the VMM IP:
		hosts[cniIface.StaticConfiguration.IPConfiguration.IPAddr.IP.String()] = handler.config.Hostname
	}

	sourceStat, err := utils.CheckIfExistsAndIsRegular(etcHostsFile)
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

	for k, v := range hosts {
		hostsLine := k + "\t" + v
		if k == "127.0.0.1" || k == "::1" {
			if cniIface == nil && handler.config.Hostname != "" {
				hostsLine = hostsLine + " " + handler.config.Hostname // TODO: change this in the future when it's possible to inject VMM IP
			}
		}
		hostsLine = hostsLine + "\n"
		written, writeErr := writableFile.WriteString(hostsLine)
		if writeErr != nil {
			handler.logger.Error("failed writing hosts to file", "reason", writeErr)
			return errors.Wrap(writeErr, "hosts file write failed: see error")
		}
		if written != len(hostsLine) {
			handler.logger.Error("hosts file bytes written != hosts length", "kv", k+"::"+v, "written", written, "required", len(hostsLine))
			return errors.New("hosts file write failed: written != length")
		}
	}

	return nil
}

func (handler *pseudoCloudInitHandlerImplementation) injectMetadata() error {
	if handler.config.Metadata == nil {
		return nil
	}
	jsonBytes, jsonErr := json.Marshal(handler.config.Metadata)
	if jsonErr != nil {
		return jsonErr
	}
	targetFile := filepath.Join(handler.mountedFsRoot, "/etc/firebuild-metadata.json")
	handler.logger.Debug("creating firebuild metadata file for writing")
	if err := handler.writeBytesAtLocation(jsonBytes, targetFile); err != nil {
		handler.logger.Error("failed writing firebuild metadata to file", "reason", err)
		return err
	}
	return nil
}

func (handler *pseudoCloudInitHandlerImplementation) injectNetInfo(cniIface *firecracker.NetworkInterface) error {
	if cniIface == nil {
		return nil
	}
	netInfoData := map[string]interface{}{
		"mac-address":   cniIface.StaticConfiguration.MacAddress,
		"host-dev-name": cniIface.StaticConfiguration.HostDevName,
		"ip-config": map[string]interface{}{
			"ip":          cniIface.StaticConfiguration.IPConfiguration.IPAddr.IP.String(),
			"ip-addr":     cniIface.StaticConfiguration.IPConfiguration.IPAddr.String(),
			"ip-mask":     cniIface.StaticConfiguration.IPConfiguration.IPAddr.Mask.String(),
			"ip-net":      cniIface.StaticConfiguration.IPConfiguration.IPAddr.Network(),
			"gateway":     cniIface.StaticConfiguration.IPConfiguration.Gateway.String(),
			"nameservers": cniIface.StaticConfiguration.IPConfiguration.Nameservers,
		},
	}
	jsonBytes, jsonErr := json.Marshal(&netInfoData)
	if jsonErr != nil {
		return jsonErr
	}
	targetFile := filepath.Join(handler.mountedFsRoot, "/etc/firebuild-netinfo.json")
	handler.logger.Debug("creating firebuild netinfo file for writing")
	if err := handler.writeBytesAtLocation(jsonBytes, targetFile); err != nil {
		handler.logger.Error("failed writing firebuild netinfo to file", "reason", err)
		return err
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

	sourceStat, err := utils.CheckIfExistsAndIsRegular(authKeysFullPath)
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

func (handler *pseudoCloudInitHandlerImplementation) writeBytesAtLocation(data []byte, location string) error {
	writableFile, fileErr := os.Create(location)
	if fileErr != nil {
		return fmt.Errorf("failed creating file '%s' for writing: %+v", location, fileErr)
	}
	defer func() {
		if err := writableFile.Close(); err != nil {
			handler.logger.Error("failed closing file AFTER writing", "file", location, "reason", err)
		}
	}()
	written, writeErr := writableFile.Write(data)
	if writeErr != nil {
		handler.logger.Error("failed writing to file", "file", location, "reason", writeErr)
		return errors.Wrap(writeErr, "file write failed: see error")
	}
	if written != len(data) {
		handler.logger.Error("file bytes written != data length", "written", written, "required", len(data))
		return errors.New("file write failed: written != length")
	}
	return nil
}
