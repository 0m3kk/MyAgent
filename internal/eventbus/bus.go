package eventbus

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"sync"

	"myagent/internal/model"
	"myagent/internal/store"
)

// Bus persists events and fans them out to connected UI clients. Persisted
// events always win over live delivery: subscribers that fall behind are
// disconnected so the client can replay from its last sequence.
type Bus struct {
	store *store.Store

	mu   sync.Mutex
	subs map[string]map[*Subscription]struct{}
}

type Subscription struct {
	Stream string
	Ch     chan model.Event

	bus    *Bus
	closed bool
}

func New(st *store.Store) *Bus {
	return &Bus{store: st, subs: map[string]map[*Subscription]struct{}{}}
}

func (b *Bus) Store() *store.Store { return b.store }

// Emit writes an event and its projection in one transaction, then broadcasts
// it. Projection is optional. Ephemeral events are never written to SQLite.
func (b *Bus) Emit(ctx context.Context, evt model.Event, projection func(*sql.Tx) error) (model.Event, error) {
	if evt.Stream == "" {
		evt.Stream = model.StreamSys
	}
	if evt.Ephemeral || strings.HasSuffix(evt.Type, ".delta") {
		evt.Ephemeral = true
		evt.ID = 0
		evt.Seq = 0
		b.broadcast(evt)
		return evt, nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	var out model.Event
	err := b.store.Tx(ctx, func(tx *sql.Tx) error {
		if projection != nil {
			if err := projection(tx); err != nil {
				return err
			}
		}
		var e error
		out, e = b.store.AppendEventTx(ctx, tx, evt)
		return e
	})
	if err != nil {
		return out, err
	}
	b.broadcastLocked(out)
	return out, nil
}

// EmitSimple persists without projection.
func (b *Bus) EmitSimple(ctx context.Context, evt model.Event) (model.Event, error) {
	return b.Emit(ctx, evt, nil)
}

func (b *Bus) Delta(stream, typ, runID, stepID string, payload any) {
	raw, _ := json.Marshal(payload)
	b.EmitNoErr(model.Event{Stream: stream, Type: typ, TS: model.Now(), RunID: runID, StepID: stepID,
		SchemaVer: 1, Payload: raw, Ephemeral: true})
}

func (b *Bus) EmitNoErr(evt model.Event) {
	if evt.Ephemeral || strings.HasSuffix(evt.Type, ".delta") {
		evt.Ephemeral = true
		b.broadcast(evt)
		return
	}
	_, err := b.Emit(context.Background(), evt, nil)
	if err != nil {
		// Last-resort visibility for operators; event persistence failures are
		// otherwise silent because there is no UI channel to report them on.
		println("eventbus: emit failed:", err.Error())
	}
}

func (b *Bus) Subscribe(stream string) *Subscription {
	s := &Subscription{Stream: stream, Ch: make(chan model.Event, 512), bus: b}
	b.mu.Lock()
	if b.subs[stream] == nil {
		b.subs[stream] = map[*Subscription]struct{}{}
	}
	b.subs[stream][s] = struct{}{}
	b.mu.Unlock()
	return s
}

func (s *Subscription) Close() {
	s.bus.mu.Lock()
	defer s.bus.mu.Unlock()
	s.closeLocked()
}

func (s *Subscription) closeLocked() {
	if s.closed {
		return
	}
	s.closed = true
	delete(s.bus.subs[s.Stream], s)
	if len(s.bus.subs[s.Stream]) == 0 {
		delete(s.bus.subs, s.Stream)
	}
	close(s.Ch)
}

func (b *Bus) broadcast(evt model.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.broadcastLocked(evt)
}

func (b *Bus) broadcastLocked(evt model.Event) {
	for sub := range b.subs[evt.Stream] {
		select {
		case sub.Ch <- evt:
		default:
			if evt.Ephemeral {
				// Deltas may be dropped; the final event carries the state.
				continue
			}
			sub.closeLocked()
		}
	}
}
