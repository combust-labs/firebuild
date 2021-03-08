package configs

import (
	"fmt"
	"io/ioutil"
	"os"
	"testing"
)

func TestEnvironmentMerger(t *testing.T) {

	env1 := map[string]string{
		"VAR1": "initial-value",
		"VAR2": "initial-value",
		"VAR3": "var3-init",
		"VAR4": "var4-init",
	}
	envFile1, err := writeEnvFile(t, env1)
	if err != nil {
		t.Error(err)
		return
	}
	defer os.Remove(envFile1.Name())

	env2 := map[string]string{
		"VAR1":        "updated-value",
		"export VAR2": "changed",
	}
	envFile2, err := writeEnvFile(t, env2)
	if err != nil {
		t.Error(err)
		return
	}
	defer os.Remove(envFile2.Name())

	env3 := map[string]string{
		"VAR3": "also-changed",
	}

	cfg := &RunCommandConfig{
		EnvFiles: []string{envFile1.Name(), envFile2.Name()},
		EnvVars:  env3,
	}

	merged, err := cfg.MergedEnvironment()
	if err != nil {
		t.Error(err)
		return
	}

	expected := map[string]string{
		"VAR1": "updated-value",
		"VAR2": "changed",
		"VAR3": "also-changed",
		"VAR4": "var4-init",
	}

	for k, v := range expected {
		vv, ok := merged[k]
		if !ok {
			t.Error("expected", k, "in merged but not found")
			return
		}
		if v != vv {
			t.Error("expected", v, "to equal", vv)
			return
		}
	}

}

func writeEnvFile(t *testing.T, env map[string]string) (*os.File, error) {
	tempFile, err := ioutil.TempFile("", "")
	if err != nil {
		return nil, err
	}
	for k, v := range env {
		if _, err := tempFile.WriteString(fmt.Sprintf("%s=%s\n", k, v)); err != nil {
			tempFile.Close()
			os.Remove(tempFile.Name())
			return nil, err
		}
	}
	tempFile.Close()
	return tempFile, nil
}
