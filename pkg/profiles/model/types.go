package model

// Profile represents a serializable profile information.
type Profile struct {
	BinaryFirecracker string `json:"binary-firecracker,omitempty" mapstructure:"binary-firecracker"`
	BinaryJailer      string `json:"binary-jailer,omitempty" mapstructure:"binary-jailer"`
	ChrootBase        string `json:"chroot-base,omitempty" mapstructure:"chroot-base"`
	RunCache          string `json:"run-cache,omitempty" mapstructure:"run-cache"`

	StorageProvider              string            `json:"storage-provider,omitempty" mapstructure:"storage-provider-type"`
	StorageProviderConfigStrings map[string]string `json:"storage-profile-config-strings,omitempty" mapstructure:"storage-profile-config-strings"`
	StorageProviderConfigInt64s  map[string]int64  `json:"storage-profile-config-int64,omitempty" mapstructure:"storage-profile-config-int64"`

	TracingEnable            bool   `json:"tracing-enable,omitempty" mapstructure:"tracing-enable"`
	TracingCollectorHostPort string `json:"tracing-collector-host-port,omitempty" mapstructure:"tracing-collector-host-port"`
	TracingLogEnable         bool   `json:"tracing-log-enable,omitempty" mapstructure:"tracing-log-enable"`
}
