package tools

import (
	"os"
	"path/filepath"
)

// sandboxDenyPaths hides the app database, the user's home directory and
// common credential stores from subprocess tools. The workspace, tool
// directory and output directory are re-allowed explicitly, so agent-authored
// tools can only touch their own workspace.
func sandboxDenyPaths(e *Executor) []string {
	home, _ := os.UserHomeDir()
	paths := []string{}
	if home != "" {
		paths = append(paths, home)
	}
	paths = append(paths,
		e.DataDir,
		filepath.Join(home, ".ssh"),
		filepath.Join(home, ".aws"),
		filepath.Join(home, ".config"),
		filepath.Join(home, ".gnupg"),
		filepath.Join(home, "Library", "Keychains"),
		filepath.Join(home, "Library", "Application Support", "MyAgent"),
		"/Library/Keychains",
		"/private/var/db/SystemKey",
	)
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
