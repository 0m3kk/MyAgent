package secrets

import (
	"fmt"
	"path/filepath"
	"strings"
)

// NewKeyProvider chooses the master-key backend. `file` is the development
// default; `keychain` is the release default and keeps the key outside SQLite
// and outside the data directory.
func NewKeyProvider(kind, dataDir string) (KeyProvider, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "", "file":
		return &FileKeyProvider{Path: filepath.Join(dataDir, "master.key")}, nil
	case "keychain", "macos-keychain":
		return newKeychainProvider()
	default:
		return nil, fmt.Errorf("unknown key provider %q (use file or keychain)", kind)
	}
}
