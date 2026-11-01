package app

import (
	"context"
	"path/filepath"
	"testing"

	"myagent/internal/model"
)

func TestModelForNameSelectsDefaultAndExplicitProfiles(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	a, err := New(ctx, Config{
		DataDir: dir, WorkspaceRoot: filepath.Join(dir, "workspace"),
		DBPath: filepath.Join(dir, "test.db"), ArtifactDir: filepath.Join(dir, "artifacts"),
		ToolsDir: filepath.Join(dir, "tools"), Version: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	cheap, err := a.SaveModelProfile(ctx, model.ModelProfile{
		Name: "Cheap", BaseURL: "https://example.com/v1", Model: "cheap-model",
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	strong, err := a.SaveModelProfile(ctx, model.ModelProfile{
		Name: "Strong", BaseURL: "https://example.com/v1", Model: "strong-model",
	}, "")
	if err != nil {
		t.Fatal(err)
	}

	got, _, err := a.ModelForName(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != cheap.ID {
		t.Fatalf("default without setting = %s, want first profile %s", got.ID, cheap.ID)
	}
	if a.GetSettings().DefaultModelID != cheap.ID {
		t.Fatalf("first saved profile should become the default")
	}

	if _, err := a.UpdateSettings(ctx, map[string]any{"default_model_id": strong.ID}); err != nil {
		t.Fatal(err)
	}
	got, _, err = a.ModelForName(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != strong.ID {
		t.Fatalf("default = %s, want %s", got.ID, strong.ID)
	}

	got, _, err = a.ModelForName(ctx, "cheap-model")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != cheap.ID {
		t.Fatalf("model by provider ID = %s, want %s", got.ID, cheap.ID)
	}

	got, _, err = a.ModelForName(ctx, "Strong")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != strong.ID {
		t.Fatalf("model by profile name = %s, want %s", got.ID, strong.ID)
	}

	if _, _, err := a.ModelForName(ctx, "missing-model"); err == nil {
		t.Fatal("expected an error for an unknown explicit model")
	}

	if _, err := a.UpdateSettings(ctx, map[string]any{
		"model_assignments": map[string]string{
			model.ModelSelectChat:     cheap.ID,
			model.ModelSelectAnalysis: strong.ID,
			model.ModelSelectOrganize: "missing-model",
		},
	}); err != nil {
		t.Fatal(err)
	}
	got, _, err = a.ModelForTask(ctx, model.ModelSelectChat)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != cheap.ID {
		t.Fatalf("chat assignment = %s, want %s", got.ID, cheap.ID)
	}
	got, _, err = a.ModelForTask(ctx, model.ModelSelectChatTitle)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != strong.ID {
		t.Fatalf("unassigned feature should use the default %s, got %s", strong.ID, got.ID)
	}
	got, _, err = a.ModelForTask(ctx, model.ModelSelectOrganize)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != strong.ID {
		t.Fatalf("stale assignment should fall back to the default %s, got %s", strong.ID, got.ID)
	}

	if err := a.DeleteModelProfile(ctx, strong.ID); err != nil {
		t.Fatal(err)
	}
	settings := a.GetSettings()
	if settings.DefaultModelID != cheap.ID {
		t.Fatalf("deleting the default profile should fall back to the first remaining profile")
	}
	if _, ok := settings.ModelAssignments[model.ModelSelectAnalysis]; ok {
		t.Fatalf(
			"deleting a model should clear assignments that point to it: %+v",
			settings.ModelAssignments,
		)
	}
	if settings.ModelAssignments[model.ModelSelectChat] != cheap.ID {
		t.Fatalf("unrelated assignments should survive: %+v", settings.ModelAssignments)
	}
}
