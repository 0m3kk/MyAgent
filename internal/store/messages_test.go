package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"myagent/internal/model"
)

func TestLastAssistantUsageSkipsZeroUsageMessages(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ws, err := st.EnsureWorkspace(ctx, "Personal", filepath.Join(dir, "workspace"))
	if err != nil {
		t.Fatal(err)
	}
	conv, err := st.CreateConversation(ctx, ws.ID, "chat")
	if err != nil {
		t.Fatal(err)
	}
	content := json.RawMessage(`{"text":"hello"}`)
	if _, err := st.AppendMessage(ctx, model.Message{
		ConversationID: conv.ID, Role: "user", Content: content,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AppendMessage(ctx, model.Message{
		ConversationID: conv.ID, Role: "assistant", Content: content,
		TokensIn: 120, TokensOut: 30,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AppendMessage(ctx, model.Message{
		ConversationID: conv.ID, Role: "assistant", Content: content,
	}); err != nil {
		t.Fatal(err)
	}
	in, out, _, ok, err := st.LastAssistantUsage(ctx, conv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || in != 120 || out != 30 {
		t.Fatalf("usage = %d/%d ok=%v", in, out, ok)
	}
}
