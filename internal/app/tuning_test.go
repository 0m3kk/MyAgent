package app

import (
	"strings"
	"testing"
)

func TestUnifiedDiffKeepsVietnameseReadable(t *testing.T) {
	base := "prompt: |\n  Viết cho người không chuyên.\n  Giữ câu ngắn.\n"
	proposed := "prompt: |\n  Viết cho người không chuyên bằng tiếng Việt.\n  Giữ câu ngắn.\n"

	diff := unifiedDiff(base, proposed)
	if diff == "" {
		t.Fatal("expected a diff")
	}
	if !strings.Contains(diff, "-") || !strings.Contains(diff, "+") {
		t.Fatalf("expected added and removed lines, got:\n%s", diff)
	}
	if !strings.Contains(diff, "Viết cho người không chuyên") {
		t.Fatalf("expected readable Vietnamese text, got:\n%s", diff)
	}
	if strings.Contains(diff, "%E1") || strings.Contains(diff, "%0A") {
		t.Fatalf("diff should not contain URL-encoded text, got:\n%s", diff)
	}
}
