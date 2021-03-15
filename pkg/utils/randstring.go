package utils

import (
	"math/rand"
)

const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
const letterDigitBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// RandStringBytes returns a random string of length n.
func RandStringBytes(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = letterBytes[rand.Intn(len(letterBytes))]
	}
	return string(b)
}

// RandStringWithDigitsBytes returns a random string of length n.
func RandStringWithDigitsBytes(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = letterDigitBytes[rand.Intn(len(letterDigitBytes))]
	}
	return string(b)
}
