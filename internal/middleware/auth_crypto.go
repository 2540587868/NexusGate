package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/json"
	"hash"
)

func cryptoSHA256() hash.Hash   { return sha256.New() }
func cryptoSHA384() hash.Hash   { return sha512.New384() }
func cryptoSHA512() hash.Hash   { return sha512.New() }

func verifyHMAC(input, signature, secret []byte, newHash func() hash.Hash) error {
	mac := hmac.New(newHash, secret)
	mac.Write(input)
	expected := mac.Sum(nil)
	if !hmac.Equal(signature, expected) {
		return hmacError("signature mismatch")
	}
	return nil
}

type hmacError string

func (e hmacError) Error() string { return string(e) }

func jsonUnmarshal(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}
