package profiles

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/combust-labs/firebuild/configs"
	"github.com/combust-labs/firebuild/pkg/profiles/model"
	"github.com/combust-labs/firebuild/pkg/utils"
	"github.com/pkg/errors"
)

// ListProfiles list available profiles. Retuns a sorted list of profile names.
func ListProfiles(location string) ([]string, error) {
	result := []string{}
	files, err := os.ReadDir(location)
	if err != nil {
		return result, errors.Wrap(err, "failed reading profiles directory")
	}
	for _, f := range files {
		if !f.IsDir() {
			if _, err := ReadProfile(f.Name(), location); err == nil {
				result = append(result, f.Name())
			}
		}
	}
	sort.Strings(result)
	return result, nil
}

// ReadProfile reads the profile information for a profile name and profile directory.
// Name is always lowercase.
func ReadProfile(name, location string) (ResolvedProfile, error) {
	profilePath := filepath.Join(location, strings.ToLower(name))
	if _, fileErr := utils.CheckIfExistsAndIsRegular(profilePath); fileErr != nil {
		if os.IsNotExist(fileErr) {
			return nil, errors.Wrap(fileErr, "profile does not exist")
		}
		return nil, errors.Wrap(fileErr, "failed checking of profile path points to an existing file")
	}
	profileBytes, readErr := ioutil.ReadFile(profilePath)
	if readErr != nil {
		return nil, errors.Wrap(readErr, "failed reading profile")
	}
	profile := &model.Profile{}
	if jsonErr := json.Unmarshal(profileBytes, profile); jsonErr != nil {
		return nil, errors.Wrap(jsonErr, "failed unmarshaling profile")
	}
	return &defaultResolvedProfile{underlying: profile}, nil
}

// WriteProfileFile writes the profile to a file named wuth `name` in the `location` directory.
// Name is always lowercase.
func WriteProfileFile(name, location string, config *configs.ProfileCreateConfig) error {
	profilePath := filepath.Join(location, strings.ToLower(name))
	dirStat, dirErr := utils.CheckIfExistsAndIsDirectory(profilePath)
	if dirErr != nil && !os.IsNotExist(dirErr) {
		if !strings.HasPrefix(dirErr.Error(), "not a directory") {
			return errors.Wrap(dirErr, "failed checking of profile path points to an existing directory")
		}
	}
	if dirStat != nil {
		return errors.New("profile path is an existing directory")
	}

	fileStat, fileErr := utils.CheckIfExistsAndIsRegular(profilePath)
	if fileErr != nil && !os.IsNotExist(fileErr) {
		return errors.Wrap(fileErr, "failed checking of profile path points to an existing file")
	}
	if fileStat != nil && !config.Overwrite {
		return errors.New("profile path is an existing file but overwrite is not allowed")
	}

	data, jsonErr := json.Marshal(config)
	if jsonErr != nil {
		return errors.Wrap(jsonErr, "failed serializing profile config")
	}
	if err := os.WriteFile(profilePath, data, 0644); err != nil {
		return errors.Wrap(jsonErr, "failed writing profile config")
	}

	return nil
}

// ResolvedProfile provides additional functionality to a profile loaded from disk.
type ResolvedProfile interface {
	// Returns a merged storage configuration.
	GetMergedStorageConfig() map[string]interface{}
	// Returns an underlying profile.
	Profile() *model.Profile
	// Updates the config of the underlying profile.
	// Changes anre not automatically persisted.
	UpdateConfigs(...configs.ProfileInheriting) error
}

type defaultResolvedProfile struct {
	underlying *model.Profile
}

// GetMergedStorageConfig returns a merged storage configuration.
func (c *defaultResolvedProfile) GetMergedStorageConfig() map[string]interface{} {
	result := map[string]interface{}{}
	for k, v := range c.underlying.StorageProviderConfigStrings {
		result[k] = v
	}
	for k, v := range c.underlying.StorageProviderConfigInt64s {
		result[k] = v
	}
	return result
}

// Profile returns an underlying profile.
func (c *defaultResolvedProfile) Profile() *model.Profile {
	return c.underlying
}

// UpdateConfigs updates the config of the underlying profile.
// Changes anre not automatically persisted.
func (c *defaultResolvedProfile) UpdateConfigs(config ...configs.ProfileInheriting) error {
	for _, cfg := range config {
		if err := cfg.UpdateFromProfile(c.underlying); err != nil {
			return err
		}
	}
	return nil
}
