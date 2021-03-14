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

type Profile struct {
	configs.ProfileCreateConfig
}

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

func ReadProfile(name, location string) (*model.Profile, error) {
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
	return profile, nil
}

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
