package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"myagent/internal/model"
)

func TestOrganizeLibraryWithAgent(t *testing.T) {
	plan := `{
  "summary": "Grouped by purpose",
  "assignments": [
    {"item": "fs.list", "folder": ["Files"]},
    {"item": "fs.read_text", "folder": ["Files"]},
    {"item": "fs.write_text", "folder": ["Files"]},
    {"item": "fs.stat", "folder": ["Files", "Metadata"]},
    {"item": "http.fetch", "folder": ["Web"]},
    {"item": "shell.exec", "folder": ["System"]},
    {"item": "workspace.info", "folder": ["System"]},
    {"item": "text.stats", "folder": ["Text"]}
  ]
}`
	var requestBody string
	llmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		requestBody = string(raw)
		w.Header().Set("Content-Type", "text/event-stream")
		chunk, _ := json.Marshal(map[string]any{
			"model": "fake",
			"choices": []any{map[string]any{
				"delta":         map[string]any{"content": "```json\n" + plan + "\n```"},
				"finish_reason": "stop",
			}},
		})
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", chunk)
	}))
	defer llmSrv.Close()

	dir := t.TempDir()
	ctx := context.Background()
	a, err := New(ctx, Config{
		DataDir: dir, WorkspaceRoot: filepath.Join(dir, "workspace"),
		DBPath: filepath.Join(dir, "test.db"), ArtifactDir: filepath.Join(dir, "artifacts"),
		ToolsDir: filepath.Join(dir, "tools"), Version: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if _, err := a.SaveModelProfile(ctx, model.ModelProfile{
		Name: "fake", BaseURL: llmSrv.URL + "/v1", Model: "fake", Role: "planner",
	}, ""); err != nil {
		t.Fatal(err)
	}

	result, err := a.OrganizeLibrary(ctx, "tool")
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary != "Grouped by purpose" || result.MovedItems != 8 ||
		result.AssignedItems != 8 {
		t.Fatalf("result = %+v", result)
	}
	if len(result.CreatedFolders) != 5 {
		t.Fatalf("created folders = %+v", result.CreatedFolders)
	}
	if !strings.Contains(requestBody, `fs.list`) ||
		!strings.Contains(requestBody, `existing_folders`) {
		t.Fatalf("prompt did not include the library inventory: %s", requestBody)
	}

	folders, err := a.Store.ListFolders(ctx, "tool")
	if err != nil {
		t.Fatal(err)
	}
	paths := folderPathMap(folders)
	byPath := map[string]string{}
	for id, path := range paths {
		byPath[strings.Join(path, "/")] = id
	}
	tools, err := a.ToolList(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools {
		want := "Files"
		switch tool.Name {
		case "fs.stat":
			want = "Files/Metadata"
		case "http.fetch":
			want = "Web"
		case "shell.exec", "workspace.info":
			want = "System"
		case "text.stats":
			want = "Text"
		}
		if tool.FolderID != byPath[want] {
			t.Fatalf("%s folder = %q, want %s", tool.Name, tool.FolderID, want)
		}
	}
}
