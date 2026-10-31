//go:build darwin && cgo

package secrets

/*
#cgo CFLAGS: -Wno-deprecated-declarations
#cgo LDFLAGS: -framework Security -framework CoreFoundation
#include <Security/Security.h>
#include <stdlib.h>
#include <string.h>

static OSStatus myagent_find_generic(const char *service, const char *account, void **data, UInt32 *len) {
	return SecKeychainFindGenericPassword(
		NULL,
		(UInt32)strlen(service), service,
		(UInt32)strlen(account), account,
		len, data, NULL);
}

static OSStatus myagent_add_generic(const char *service, const char *account, const void *data, UInt32 len) {
	return SecKeychainAddGenericPassword(
		NULL,
		(UInt32)strlen(service), service,
		(UInt32)strlen(account), account,
		len, data, NULL);
}

static void myagent_free_content(void *data) {
	SecKeychainItemFreeContent(NULL, data);
}
*/
import "C"

import (
	"context"
	"crypto/rand"
	"fmt"
	"unsafe"
)

const (
	errSecSuccess      = 0
	errSecItemNotFound = -25300
)

// KeychainKeyProvider stores the 32-byte master key in the login keychain as a
// generic password. The key never appears in argv, logs, or the SQLite file.
type KeychainKeyProvider struct {
	Service string
	Account string
}

func newKeychainProvider() (KeyProvider, error) {
	return &KeychainKeyProvider{Service: "com.myagent.desktop", Account: "master-key"}, nil
}

func (p *KeychainKeyProvider) Kind() string { return "keychain" }
func (p *KeychainKeyProvider) Description() string {
	return "macOS Keychain (generic password, app access only)."
}

func (p *KeychainKeyProvider) MasterKey(ctx context.Context) ([]byte, error) {
	service := C.CString(p.Service)
	account := C.CString(p.Account)
	defer C.free(unsafe.Pointer(service))
	defer C.free(unsafe.Pointer(account))

	var data unsafe.Pointer
	var length C.UInt32
	status := C.myagent_find_generic(service, account, &data, &length)
	if status == errSecSuccess {
		defer C.myagent_free_content(data)
		if int(length) != 32 {
			return nil, fmt.Errorf("keychain master key has %d bytes, expected 32", int(length))
		}
		key := C.GoBytes(data, C.int(length))
		return key, nil
	}
	if status != errSecItemNotFound {
		return nil, fmt.Errorf("keychain lookup failed with OSStatus %d", int(status))
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	status = C.myagent_add_generic(service, account, unsafe.Pointer(&key[0]), C.UInt32(len(key)))
	if status != errSecSuccess {
		return nil, fmt.Errorf("keychain write failed with OSStatus %d", int(status))
	}
	return key, nil
}
