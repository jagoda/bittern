package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
)

const (
	// PasswordIterations is the PBKDF2-HMAC-SHA256 iteration count used for
	// new administrator passwords. The count is stored with each hash so a
	// later release can raise it without breaking existing records.
	PasswordIterations = 600000
	passwordKeyLength  = 32
	passwordSaltLength = 16
	maxIterations      = 5000000
)

type passwordRecord struct {
	Algorithm  string `json:"algorithm"`
	Iterations int    `json:"iterations"`
	Salt       string `json:"salt"`
	Hash       string `json:"hash"`
}

func newPasswordRecord(password string, iterations int, random io.Reader) (passwordRecord, error) {
	if iterations <= 0 || iterations > maxIterations {
		return passwordRecord{}, fmt.Errorf("auth: invalid password iteration count %d", iterations)
	}
	if random == nil {
		random = rand.Reader
	}
	salt := make([]byte, passwordSaltLength)
	if _, err := io.ReadFull(random, salt); err != nil {
		return passwordRecord{}, err
	}
	hash := deriveKey([]byte(password), salt, iterations, passwordKeyLength)
	return passwordRecord{
		Algorithm:  "pbkdf2-hmac-sha256",
		Iterations: iterations,
		Salt:       base64.StdEncoding.EncodeToString(salt),
		Hash:       base64.StdEncoding.EncodeToString(hash),
	}, nil
}

func verifyPassword(record passwordRecord, password string) bool {
	if record.Algorithm != "pbkdf2-hmac-sha256" || record.Iterations <= 0 ||
		record.Iterations > maxIterations || record.Salt == "" || record.Hash == "" {
		return false
	}
	salt, err := base64.StdEncoding.DecodeString(record.Salt)
	if err != nil {
		return false
	}
	want, err := base64.StdEncoding.DecodeString(record.Hash)
	if err != nil || len(want) == 0 {
		return false
	}
	got := deriveKey([]byte(password), salt, record.Iterations, len(want))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// deriveKey is PBKDF2 with HMAC-SHA256. The standard library does not export
// PBKDF2, and the project vendors no x/crypto package, so the small derivation
// loop lives here.
func deriveKey(password, salt []byte, iterations, keyLength int) []byte {
	hashLength := sha256.Size
	blocks := (keyLength + hashLength - 1) / hashLength
	result := make([]byte, 0, blocks*hashLength)
	u := make([]byte, hashLength)
	t := make([]byte, hashLength)
	var index [4]byte

	for block := 1; block <= blocks; block++ {
		mac := hmac.New(sha256.New, password)
		_, _ = mac.Write(salt)
		binary.BigEndian.PutUint32(index[:], uint32(block))
		_, _ = mac.Write(index[:])
		mac.Sum(u[:0])
		copy(t, u)

		for round := 1; round < iterations; round++ {
			mac.Reset()
			_, _ = mac.Write(u)
			mac.Sum(u[:0])
			for position := range t {
				t[position] ^= u[position]
			}
		}
		result = append(result, t...)
	}
	return result[:keyLength]
}
