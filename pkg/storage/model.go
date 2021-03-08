package storage

import "github.com/spf13/pflag"

// FlagProvider defines an interface for the policy storage provider flag handling.
type FlagProvider interface {
	GetFlags() *pflag.FlagSet
	GetInitializedConfiguration() map[string]interface{}
}

// KernelLookup is the kernel query parameters configuration.
type KernelLookup struct {
	ID string
}

// KernelResult contains the information about the resolved kernel.
type KernelResult interface {
	HostPath() string
	Metadata() map[string]interface{}
}

// RootfsLookup is the rootfs query parameters configuration.
type RootfsLookup struct {
	Org     string
	Image   string
	Version string
}

type RootfsStore struct {
	LocalPath string
	Metadata  map[string]interface{}

	Org     string
	Image   string
	Version string
}

// RootfsResult contains the information about the resolved rootfs.
type RootfsResult interface {
	HostPath() string
	Metadata() map[string]interface{}
}

// RootfsStoreResult contains the information about the stored rootfs.
type RootfsStoreResult struct {
	MetadataLocation string
	Provider         string
	RootfsLocation   string
}

// Provider represents a storage provider.
type Provider interface {
	Configure(map[string]interface{}) error

	// FetchKernel fetches a Linux Kernel by ID.
	FetchKernel(*KernelLookup) (KernelResult, error)
	// FetchRootfs fetches a root file system by ID.
	FetchRootfs(*RootfsLookup) (RootfsResult, error)

	StoreRootfsFile(*RootfsStore) (*RootfsStoreResult, error)
}
