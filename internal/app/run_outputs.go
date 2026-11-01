package app

import (
	"context"
	"encoding/json"
)

// RunOutputs returns the workflow outputs recorded in the run.finished event.
func (a *App) RunOutputs(ctx context.Context, runID string) (map[string]any, error) {
	events, err := a.Store.RunEvents(ctx, runID, 0, 1000)
	if err != nil {
		return nil, err
	}
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type != "run.finished" {
			continue
		}
		var p struct {
			Outputs map[string]any `json:"outputs"`
		}
		if json.Unmarshal(events[i].Payload, &p) == nil {
			return p.Outputs, nil
		}
	}
	return map[string]any{}, nil
}
