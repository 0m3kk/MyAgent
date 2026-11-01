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

// folderPathArg turns an agent-supplied folder path into cleaned segments. It
// accepts "Research/Sources" and ["Research", "Sources"] so a model can use
// either shape without a retry.
func folderPathArg(args map[string]any, key string) ([]string, error) {
	raw, ok := args[key]
	if !ok || raw == nil {
		return nil, nil
	}
	path := []string{}
	switch value := raw.(type) {
	case string:
		path = strings.FieldsFunc(value, func(r rune) bool { return r == '/' || r == '>' })
	case []string:
		path = append(path, value...)
	case []any:
		for _, item := range value {
			part, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("%s must be a folder path such as Research/Sources", key)
			}
			path = append(path, part)
		}
	default:
		return nil, fmt.Errorf("%s must be a folder path such as Research/Sources", key)
	}
	return cleanOrganizePath(organizePath(path))
}

func folderLabel(path []string) string {
	return strings.Join(path, " / ")
}

// assignToFolderPath creates any missing folders in the path, then moves the
// tool or workflow into the deepest folder.
func (a *App) assignToFolderPath(
	ctx context.Context,
	kind, itemID string,
	path []string,
) (string, []string, error) {
	if len(path) == 0 {
		return "", nil, nil
	}
	folderID, created, err := a.ensureFolderPath(ctx, kind, path)
	if err != nil {
		return "", nil, err
	}
	switch kind {
	case "tool":
		err = a.SetToolOrganization(ctx, itemID, &folderID, nil)
	case "workflow":
		err = a.SetWorkflowOrganization(ctx, itemID, &folderID, nil)
	default:
		err = fmt.Errorf("%w: kind must be tool or workflow", ErrInvalidFolder)
	}
	if err != nil {
		return "", nil, err
	}
	return folderID, created, nil
}

// ensureFolderPath reuses an existing path case-insensitively and creates only
// the missing segments.
func (a *App) ensureFolderPath(
	ctx context.Context,
	kind string,
	path []string,
) (string, []string, error) {
	cleaned, err := cleanOrganizePath(organizePath(path))
	if err != nil {
		return "", nil, err
	}
	if len(cleaned) == 0 {
		return "", nil, nil
	}
	folders, err := a.Store.ListFolders(ctx, kind)
	if err != nil {
		return "", nil, err
	}
	index := map[string]string{}
	for id, itemPath := range folderPathMap(folders) {
		index[pathKey(itemPath)] = id
	}
	created := []string{}
	createdSet := map[string]bool{}
	folderID, err := a.ensureOrganizePath(ctx, kind, cleaned, index, &created, createdSet)
	if err != nil {
		return "", nil, err
	}
	return folderID, created, nil
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
