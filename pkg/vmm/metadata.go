package vmm

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/combust-labs/firebuild/pkg/metadata"
	"github.com/combust-labs/firebuild/pkg/utils"
	"github.com/pkg/errors"
)

// FetchMetadataIfExists fetches the metadata from a metadata file in the required directory, if the file exists.
// Returns a MDRun pointer, if file exists, a boolean indicating if metadata file existed and an error,
// if metadata lookup went wrong.
func FetchMetadataIfExists(cacheDirectory string) (*metadata.MDRun, bool, error) {
	pidFile := filepath.Join(cacheDirectory, "metadata.json")
	if _, err := utils.CheckIfExistsAndIsRegular(pidFile); err != nil {
		if !os.IsNotExist(err) {
			return nil, false, err
		}
		if os.IsNotExist(err) {
			return nil, false, nil
		}
	}
	jsonBytes, err := ioutil.ReadFile(pidFile)
	if err != nil {
		return nil, false, err
	}
	results := &metadata.MDRun{}
	if jsonErr := json.Unmarshal(jsonBytes, results); jsonErr != nil {
		return nil, false, jsonErr
	}
	return results, true, nil
}

// WriteMetadataToFile writes a run metadata to file under the cache directory.
func WriteMetadataToFile(md *metadata.MDRun) error {
	mdBytes, jsonErr := json.Marshal(md)
	if jsonErr != nil {
		return errors.Wrap(jsonErr, "failed serializing machine metadata to JSON")
	}
	if err := ioutil.WriteFile(filepath.Join(md.RunCache, "metadata.json"), []byte(mdBytes), 0644); err != nil {
		return errors.Wrap(err, "failed writing PID metadata the cache file")
	}
	return nil
}
