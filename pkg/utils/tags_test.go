package utils

import (
	"fmt"
	"testing"
)

func TestTagDecompose(t *testing.T) {

	expectedOrg := "combust-labs"
	expectedImg := "image-name"
	expectedVer := "version"

	ok, org, image, version := TagDecompose(fmt.Sprintf("%s/%s:%s", expectedOrg, expectedImg, expectedVer))
	if !ok {
		t.Fatal("expected tag to decompose")
	}
	if org != expectedOrg {
		t.Fatalf("expected different than parsed: %q vs %q", expectedOrg, org)
	}
	if image != expectedImg {
		t.Fatalf("expected different than parsed: %q vs %q", expectedImg, image)
	}
	if version != expectedVer {
		t.Fatalf("expected different than parsed: %q vs %q", expectedVer, version)
	}
}
