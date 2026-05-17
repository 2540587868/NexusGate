package util

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

func GenerateRandomID(byteLen int) string {
	b := make([]byte, byteLen)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand.Read failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

func GenerateRandomHexID(byteLen int) string {
	b := make([]byte, byteLen)
	if _, err := rand.Read(b); err != nil {
		for i := range b {
			b[i] = byte(i ^ 0x5a)
		}
	}
	return fmt.Sprintf("%0*x", byteLen*2, b)
}
