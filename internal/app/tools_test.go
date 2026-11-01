package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"myagent/internal/model"
	"myagent/internal/testutil"
)

const toolVersioningManifest = `schema: 1
name: versioning.tool
description: Tool used by the versioning test.
kind: starlark
runtime: {entry: main.star}
side_effect: none
input_schema:
  type: object
  additionalProperties: false
  properties: {}
output_schema:
  type: object
  properties: {}
permissions:
  network: {domains: []}
  fs: {read: [], write: []}
  exec: []
secrets: []
limits: {timeout_s: 10, memory_mb: 64, output_kb: 64}
`

func TestToolDraftEditsDoNotConsumeVersions(t *testing.T) {
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

	first, err := a.CreateToolDraft(ctx, toolVersioningManifest, "user", 0)
	if err != nil {
		t.Fatalf("create draft: %v %+v", err, first.Issues)
	}
	if first.Version.Version != 1 {
		t.Fatalf("first version = %d", first.Version.Version)
	}
	editedManifest := strings.Replace(
		toolVersioningManifest,
		"Tool used by the versioning test.",
		"Edited draft manifest.",
		1,
	)
	second, err := a.CreateToolDraft(ctx, editedManifest, "user", 0)
	if err != nil {
		t.Fatal(err)
	}
	if second.Version.ID != first.Version.ID || second.Version.Version != 1 {
		t.Fatalf("draft edit created a new version: %+v -> %+v", first.Version, second.Version)
	}

	tool, err := a.Store.GetToolByID(ctx, first.Tool.ID)
	if err != nil {
		t.Fatal(err)
	}
	if tool.DraftVersionID != first.Version.ID {
		t.Fatalf("draft_version_id = %q", tool.DraftVersionID)
	}
	if err := a.Store.UpdateToolVersionStatus(
		ctx,
		first.Version.ID,
		model.ToolTested,
		0,
	); err != nil {
		t.Fatal(err)
	}
	if err := a.ApproveToolVersion(ctx, first.Tool.ID, first.Version.ID); err != nil {
		t.Fatal(err)
	}
	tool, err = a.Store.GetToolByID(ctx, first.Tool.ID)
	if err != nil {
		t.Fatal(err)
	}
	if tool.CurrentVersionID != first.Version.ID || tool.DraftVersionID != "" {
		t.Fatalf("after approval current=%q draft=%q", tool.CurrentVersionID, tool.DraftVersionID)
	}

	third, err := a.CreateToolDraft(ctx, editedManifest, "user", 1)
	if err != nil {
		t.Fatal(err)
	}
	if third.Version.Version != 2 || third.Version.ID == first.Version.ID {
		t.Fatalf("expected a new draft v2, got %+v", third.Version)
	}
	tool, err = a.Store.GetToolByID(ctx, first.Tool.ID)
	if err != nil {
		t.Fatal(err)
	}
	if tool.CurrentVersionID != first.Version.ID || tool.DraftVersionID != third.Version.ID {
		t.Fatalf("after new draft current=%q draft=%q", tool.CurrentVersionID, tool.DraftVersionID)
	}
}

func TestDeletedToolsAreNotRecreatedOnRestart(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	cfg := Config{
		DataDir: dir, WorkspaceRoot: filepath.Join(dir, "workspace"),
		DBPath: filepath.Join(dir, "test.db"), ArtifactDir: filepath.Join(dir, "artifacts"),
		ToolsDir: filepath.Join(dir, "tools"), Version: "test",
	}
	a, err := New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	list, err := a.ToolList(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("fresh install should not seed tools: %+v", list)
	}
	installed, err := testutil.InstallTool(
		ctx, a.Store, a.Workspace.ID, a.cfg.ToolsDir,
		testutil.FSListManifest, testutil.FSListCode,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Store.DeleteTool(ctx, installed.ID); err != nil {
		t.Fatal(err)
	}
	a.Close()

	a, err = New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	list, err = a.ToolList(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("deleted tools were recreated: %+v", list)
	}
}
