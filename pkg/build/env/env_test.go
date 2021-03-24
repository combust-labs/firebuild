package env

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildEnv(t *testing.T) {
	inputMap := map[string]string{
		"ENV_1": "value 1",
		"ENV_2": "value 2",
	}
	buildEnv := NewBuildEnv()
	for k, v := range inputMap {
		buildEnv.Put(k, v)
	}
	assert.Equal(t, inputMap, buildEnv.Snapshot())

	expected := "Expecting value 1 and value 2 but not apkArch=\"$(apk --print-arch)\" && case \"${apkArch}\""
	output := buildEnv.Expand("Expecting ${ENV_1} and ${ENV_2} but not apkArch=\"$(apk --print-arch)\" && case \"${apkArch}\"")
	assert.Equal(t, output, expected)

}
