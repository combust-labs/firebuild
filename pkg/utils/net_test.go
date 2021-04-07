package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfiguredOrSuitable(t *testing.T) {
	expected := "configured"
	ifaceName, err := GetConfiguredOrSuitableInterfaceName(expected)
	assert.Nil(t, err)
	assert.Equal(t, expected, ifaceName)

	ifaceName2, err2 := GetConfiguredOrSuitableInterfaceName("")
	assert.Nil(t, err2)
	expectedIface, err3 := GetFirstUpBroadcastInterface()
	assert.Nil(t, err3)

	assert.Equal(t, expectedIface.Name, ifaceName2)
}

func TestSuitableInterfacesFetching(t *testing.T) {
	iface, err := GetFirstUpBroadcastInterface()
	assert.Nil(t, err)
	assert.NotEmpty(t, iface.Name)
	t.Log(" ====> ", iface.Name, iface.Flags.String(), iface.Index)
}
