package ids

import (
	"time"

	"github.com/google/uuid"
)

// New returns a time-ordered UUIDv7 string. IDs sort lexicographically by creation
// time, which keeps SQLite b-tree inserts friendly.
func New() string {
	id, err := uuid.NewV7()
	if err != nil {
		// UUIDv7 fails only when the entropy source is unavailable.
		return uuid.NewString()
	}
	return id.String()
}

// Short returns a short human-friendly ID suffix (used for display only).
func Short(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

func NowMS() int64 { return time.Now().UnixMilli() }

func ParseMS(s string) (int64, bool) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return 0, false
	}
	return t.UnixMilli(), true
}
