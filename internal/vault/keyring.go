package vault

import (
	"encoding/base64"
	"errors"

	"github.com/zalando/go-keyring"
)

const keyringService = "NyaTerminal"

func SaveQuickUnlock(profile string, wrappedUnlockMaterial []byte) error {
	if profile == "" || len(wrappedUnlockMaterial) == 0 {
		return errors.New("invalid quick unlock material")
	}
	return keyring.Set(keyringService, profile, base64.RawStdEncoding.EncodeToString(wrappedUnlockMaterial))
}

func LoadQuickUnlock(profile string) ([]byte, error) {
	value, err := keyring.Get(keyringService, profile)
	if err != nil {
		return nil, err
	}
	return base64.RawStdEncoding.DecodeString(value)
}

func DeleteQuickUnlock(profile string) error {
	return keyring.Delete(keyringService, profile)
}
