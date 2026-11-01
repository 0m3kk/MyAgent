package tools

import (
	"os"
	"strings"
	"testing"
)

func TestSandboxProfileScopesReadsToWorkspace(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home directory")
	}
	workspace := t.TempDir()
	e := NewExecutor(workspace, t.TempDir())
	profile := sandboxProfile(SandboxSpec{
		Workspace: workspace,
		ToolDir:   t.TempDir(),
		OutDir:    t.TempDir(),
		DenyRead:  sandboxDenyPaths(e),
	})
	denyHome := `(deny file-read* (subpath "` + sandboxPath(home) + `"))`
	allowWorkspace := `(allow file-read* (subpath "` + sandboxPath(workspace) + `"))`
	denyAt := strings.Index(profile, denyHome)
	allowAt := strings.Index(profile, allowWorkspace)
	if denyAt < 0 || allowAt < 0 || denyAt > allowAt {
		t.Fatalf("profile does not deny home before allowing the workspace:\n%s", profile)
	}
}
