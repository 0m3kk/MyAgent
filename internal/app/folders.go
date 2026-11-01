package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"myagent/internal/model"
)

var ErrInvalidFolder = errors.New("invalid folder")

func (a *App) FolderList(ctx context.Context, kind string) ([]model.Folder, error) {
	return a.Store.ListFolders(ctx, kind)
}

func (a *App) CreateFolder(
	ctx context.Context,
	kind, name, parentID string,
) (model.Folder, error) {
	kind = strings.TrimSpace(kind)
	if kind != "tool" && kind != "workflow" {
		return model.Folder{}, fmt.Errorf("%w: kind must be tool or workflow", ErrInvalidFolder)
	}
	name, err := cleanFolderName(name)
	if err != nil {
		return model.Folder{}, err
	}
	if parentID != "" {
		if _, err := a.validFolderParent(ctx, a.Workspace.ID, kind, parentID); err != nil {
			return model.Folder{}, err
		}
	}
	exists, err := a.Store.FolderNameExists(ctx, a.Workspace.ID, kind, parentID, name, "")
	if err != nil {
		return model.Folder{}, err
	}
	if exists {
		return model.Folder{}, fmt.Errorf(
			"%w: a folder with that name already exists here",
			ErrInvalidFolder,
		)
	}
	folder, err := a.Store.CreateFolder(ctx, model.Folder{
		WorkspaceID: a.Workspace.ID,
		Kind:        kind,
		Name:        name,
		ParentID:    parentID,
	})
	if err != nil {
		return model.Folder{}, err
	}
	a.Bus.EmitNoErr(model.Event{Stream: model.StreamSys, Type: "folder.created",
		Payload: a.mustPayload(map[string]any{
			"folder_id": folder.ID, "kind": folder.Kind, "name": folder.Name,
			"parent_id": folder.ParentID,
		})})
	return folder, nil
}

func (a *App) UpdateFolder(
	ctx context.Context,
	id string,
	name *string,
	parentID *string,
) (model.Folder, error) {
	folder, err := a.Store.GetFolder(ctx, id)
	if err != nil {
		return model.Folder{}, err
	}
	if name != nil {
		folder.Name, err = cleanFolderName(*name)
		if err != nil {
			return model.Folder{}, err
		}
	}
	if parentID != nil {
		nextParent := strings.TrimSpace(*parentID)
		if nextParent != "" {
			if _, err := a.validFolderParent(
				ctx,
				folder.WorkspaceID,
				folder.Kind,
				nextParent,
			); err != nil {
				return model.Folder{}, err
			}
			inside, err := a.folderInSubtree(ctx, nextParent, folder.ID)
			if err != nil {
				return model.Folder{}, err
			}
			if inside {
				return model.Folder{}, fmt.Errorf(
					"%w: a folder cannot be moved inside itself",
					ErrInvalidFolder,
				)
			}
		}
		folder.ParentID = nextParent
	}
	exists, err := a.Store.FolderNameExists(
		ctx, folder.WorkspaceID, folder.Kind, folder.ParentID, folder.Name, folder.ID,
	)
	if err != nil {
		return model.Folder{}, err
	}
	if exists {
		return model.Folder{}, fmt.Errorf(
			"%w: a folder with that name already exists here",
			ErrInvalidFolder,
		)
	}
	if err := a.Store.UpdateFolder(ctx, folder); err != nil {
		return model.Folder{}, err
	}
	a.Bus.EmitNoErr(model.Event{Stream: model.StreamSys, Type: "folder.updated",
		Payload: a.mustPayload(map[string]any{
			"folder_id": folder.ID, "kind": folder.Kind, "name": folder.Name,
			"parent_id": folder.ParentID,
		})})
	return folder, nil
}

func (a *App) DeleteFolder(ctx context.Context, id string) error {
	folder, err := a.Store.GetFolder(ctx, id)
	if err != nil {
		return err
	}
	if err := a.Store.DeleteFolder(ctx, id); err != nil {
		return err
	}
	a.Bus.EmitNoErr(model.Event{Stream: model.StreamSys, Type: "folder.deleted",
		Payload: a.mustPayload(map[string]any{
			"folder_id": folder.ID, "kind": folder.Kind, "name": folder.Name,
			"parent_id": folder.ParentID,
		})})
	return nil
}

func (a *App) SetToolOrganization(
	ctx context.Context,
	id string,
	folderID *string,
	pinned *bool,
) error {
	if folderID == nil && pinned == nil {
		return errors.New("nothing to update")
	}
	tool, err := a.Store.GetToolByID(ctx, id)
	if err != nil {
		return err
	}
	nextFolder := tool.FolderID
	if folderID != nil {
		nextFolder = strings.TrimSpace(*folderID)
		if nextFolder != "" {
			if _, err := a.validFolderParent(
				ctx,
				tool.WorkspaceID,
				"tool",
				nextFolder,
			); err != nil {
				return err
			}
		}
	}
	if err := a.Store.SetToolOrganization(ctx, tool.ID, nextFolder, pinned); err != nil {
		return err
	}
	payload := map[string]any{"tool_id": tool.ID, "folder_id": nextFolder}
	if pinned != nil {
		payload["pinned"] = *pinned
	}
	a.Bus.EmitNoErr(model.Event{Stream: model.StreamSys, Type: "tool.organization.updated",
		Payload: a.mustPayload(payload)})
	return nil
}

func (a *App) SetWorkflowOrganization(
	ctx context.Context,
	id string,
	folderID *string,
	pinned *bool,
) error {
	if folderID == nil && pinned == nil {
		return errors.New("nothing to update")
	}
	workflow, err := a.Store.GetWorkflowByID(ctx, id)
	if err != nil {
		return err
	}
	nextFolder := workflow.FolderID
	if folderID != nil {
		nextFolder = strings.TrimSpace(*folderID)
		if nextFolder != "" {
			if _, err := a.validFolderParent(
				ctx,
				workflow.WorkspaceID,
				"workflow",
				nextFolder,
			); err != nil {
				return err
			}
		}
	}
	if err := a.Store.SetWorkflowOrganization(ctx, workflow.ID, nextFolder, pinned); err != nil {
		return err
	}
	payload := map[string]any{"workflow_id": workflow.ID, "folder_id": nextFolder}
	if pinned != nil {
		payload["pinned"] = *pinned
	}
	a.Bus.EmitNoErr(model.Event{Stream: model.StreamSys, Type: "workflow.organization.updated",
		Payload: a.mustPayload(payload)})
	return nil
}

func cleanFolderName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("%w: name is required", ErrInvalidFolder)
	}
	if utf8.RuneCountInString(name) > 80 {
		return "", fmt.Errorf("%w: name is longer than 80 characters", ErrInvalidFolder)
	}
	if strings.ContainsAny(name, "\r\n") {
		return "", fmt.Errorf("%w: name cannot contain line breaks", ErrInvalidFolder)
	}
	return name, nil
}

func (a *App) validFolderParent(
	ctx context.Context,
	workspaceID, kind, folderID string,
) (model.Folder, error) {
	folder, err := a.Store.GetFolder(ctx, folderID)
	if err != nil {
		return model.Folder{}, fmt.Errorf("%w: parent folder does not exist", ErrInvalidFolder)
	}
	if folder.WorkspaceID != workspaceID {
		return model.Folder{}, fmt.Errorf(
			"%w: folder belongs to another workspace",
			ErrInvalidFolder,
		)
	}
	if folder.Kind != kind {
		return model.Folder{}, fmt.Errorf(
			"%w: folder is for %ss, not %ss",
			ErrInvalidFolder,
			folder.Kind,
			kind,
		)
	}
	return folder, nil
}

// folderInSubtree reports whether candidate is root or one of its descendants.
func (a *App) folderInSubtree(ctx context.Context, candidate, root string) (bool, error) {
	current := candidate
	for steps := 0; current != ""; steps++ {
		if steps > 1000 {
			return false, fmt.Errorf("%w: folder tree is too deep", ErrInvalidFolder)
		}
		if current == root {
			return true, nil
		}
		folder, err := a.Store.GetFolder(ctx, current)
		if err != nil {
			return false, err
		}
		current = folder.ParentID
	}
	return false, nil
}
