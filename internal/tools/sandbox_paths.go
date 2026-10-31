package tools

import (
	"os"
	"path/filepath"
)

// sandboxDenyPaths hides the app database, master key and common credential
// stores from subprocess tools. The workspace and tool directory are re-allowed
// explicitly even when they live under the data directory (development mode).
func sandboxDenyPaths(e *Executor) []string {
	home, _ := os.UserHomeDir()
	paths := []string{
		e.DataDir,
		filepath.Join(home, ".ssh"),
		filepath.Join(home, ".aws"),
		filepath.Join(home, ".config"),
		filepath.Join(home, ".gnupg"),
		filepath.Join(home, "Library", "Keychains"),
		filepath.Join(home, "Library", "Application Support", "MyAgent"),
		"/Library/Keychains",
		"/private/var/db/SystemKey",
	}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
