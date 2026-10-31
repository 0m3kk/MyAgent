//go:build !darwin || !cgo

package secrets

import "errors"

func newKeychainProvider() (KeyProvider, error) {
	return nil, errors.New("macOS Keychain is only available in a darwin build with cgo enabled")
}
