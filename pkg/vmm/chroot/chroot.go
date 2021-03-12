package chroot

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/combust-labs/firebuild/pkg/utils"
)

// Location represents the chroot location on disk.
type Location struct {
	ChrootBase string
	FcBinary   string
	VMMID      string
	fullPath   string
}

// FullPath returns a full path of the location.
func (o *Location) FullPath() string {
	return o.fullPath
}

// LocationFromFullPath deconstructs a full path to the location.
func LocationFromFullPath(path string) *Location {
	vmmID, p := filepath.Split(path)
	fcBinary, p := filepath.Split(p)
	chrootBase, _ := filepath.Split(p)
	return &Location{
		ChrootBase: chrootBase,
		FcBinary:   fcBinary,
		VMMID:      vmmID,
		fullPath:   path,
	}
}

// LocationFromComponents constructs a location using explicit components.
func LocationFromComponents(base, fcBinary, vmmID string) *Location {
	return &Location{
		ChrootBase: base,
		FcBinary:   filepath.Base(fcBinary),
		VMMID:      vmmID,
		fullPath:   filepath.Join(base, filepath.Base(fcBinary), vmmID),
	}
}

// Chroot represents the chroot.
type Chroot interface {
	// Checks if chroot exists.
	Exists() (bool, error)
	// Returns a full chroot location path.
	FullPath() string
	// Checks is chroot looks like a valid chroot.
	IsValid() error
	// Removes the chroot with all contents.
	RemoveAll() error
	// Returns a socket file path, if file exists.
	SocketPathIfExists() (string, bool, error)
}

// NewWithLocation returns a chroot with the configured location.
func NewWithLocation(loc *Location) Chroot {
	return &defaultChroot{
		loc: loc,
	}
}

type defaultChroot struct {
	loc *Location
}

func (c *defaultChroot) Exists() (bool, error) {
	if _, err := utils.CheckIfExistsAndIsDirectory(c.loc.FullPath()); err != nil {
		if os.IsNotExist(err) {
			// nothing to do
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (c *defaultChroot) FullPath() string {
	return c.loc.FullPath()
}

func (c *defaultChroot) IsValid() error {
	expectedEntries := map[string]bool{
		"/root":                                 true,
		"/root/dev":                             true,
		"/root/dev/kvm":                         false,
		"/root/dev/net":                         true,
		"/root/dev/net/tun":                     false,
		fmt.Sprintf("/root/%s", c.loc.FcBinary): false,
		"/root/rootfs":                          false,
		"/root/run":                             true,
		"/root/run/firecracker.socket":          false,
		// we don't know what's the vmlinux file name
	}
	foundEntries := map[string]bool{}
	filepath.WalkDir(c.FullPath(), func(path string, d fs.DirEntry, e error) error {
		foundEntries[strings.TrimPrefix(path, c.FullPath())] = d.IsDir()
		return e
	})
	for k, v := range expectedEntries {
		vv, ok := foundEntries[k]
		if !ok {
			return fmt.Errorf("vmm chroot item '%s' not found", k)
		}
		if v != vv {
			return fmt.Errorf("vmm chroot item '%s' directory status: expected %v, got %v", k, v, vv)
		}
	}
	return nil
}

func (c *defaultChroot) RemoveAll() error {
	return os.RemoveAll(c.FullPath())
}

// SocketPathIfExists fetches the VMM socket path.
// Returns the socket path, a boolean indicating if the socket exists and an error if existence check went wrong.
func (c *defaultChroot) SocketPathIfExists() (string, bool, error) {
	socketPath := filepath.Join(c.FullPath(), "root/run/firecracker.socket")
	hasSocket, existsErr := utils.PathExists(socketPath)
	return socketPath, hasSocket, existsErr
}
