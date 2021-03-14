package utils

import (
	"math/rand"
	"time"
)

// RootFSCopyBufferSize is the buffer size for root file system copy operation.
const RootFSCopyBufferSize = 4 * 1024 * 1024

func init() {
	rand.Seed(time.Now().UTC().UnixNano())
}
