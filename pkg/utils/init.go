package utils

import (
	"math/rand"
	"time"
)

func init() {
	rand.Seed(time.Now().UTC().UnixNano())
}
