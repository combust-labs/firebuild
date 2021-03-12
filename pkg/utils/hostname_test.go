package utils

import "testing"

func TestHostnameValidation(t *testing.T) {
	randomHostname := RandomHostname()
	if !IsValidHostname(randomHostname) {
		t.Error("expected the random hostname to be valid")
	}

	invalidHostname := "invalid_hostname"
	if IsValidHostname(invalidHostname) {
		t.Error("expected an invalid hostname to not validate")
	}
}
