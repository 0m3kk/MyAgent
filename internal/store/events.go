package store

import (
	"context"
	"database/sql"
	"encoding/json"

	"myagent/internal/ids"
	"myagent/internal/model"
)

// AppendEventTx assigns the next per-stream sequence and writes the event. It is
// always called inside the same transaction as the projection update.
func (s *Store) AppendEventTx(
	ctx context.Context,
	tx *sql.Tx,
	evt model.Event,
) (model.Event, error) {
	if evt.TS == 0 {
		evt.TS = ids.NowMS()
	}
	if evt.SchemaVer == 0 {
		evt.SchemaVer = 1
	}
	if len(evt.Payload) == 0 {
		evt.Payload = json.RawMessage(`{}`)
	}
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0)+1 FROM events WHERE stream=?`, evt.Stream).
		Scan(&evt.Seq); err != nil {
		return evt, err
	}
	res, err := tx.ExecContext(
		ctx,
		`INSERT INTO events (stream,seq,type,ts,run_id,step_id,schema_ver,payload)
		VALUES (?,?,?,?,?,?,?,?)`,
		evt.Stream,
		evt.Seq,
		evt.Type,
		evt.TS,
		nullString(evt.RunID),
		nullString(evt.StepID),
		evt.SchemaVer,
		string(evt.Payload),
	)
	if err != nil {
		return evt, err
	}
	evt.ID, _ = res.LastInsertId()
	return evt, nil
}

func (s *Store) AppendEvent(ctx context.Context, evt model.Event) (model.Event, error) {
	var out model.Event
	err := s.Tx(ctx, func(tx *sql.Tx) error {
		var e error
		out, e = s.AppendEventTx(ctx, tx, evt)
		return e
	})
	return out, err
}

func scanEvents(rows *sql.Rows) ([]model.Event, error) {
	defer rows.Close()
	out := []model.Event{}
	for rows.Next() {
		var e model.Event
		var payload string
		err := rows.Scan(
			&e.ID,
			&e.Stream,
			&e.Seq,
			&e.Type,
			&e.TS,
			&e.RunID,
			&e.StepID,
			&e.SchemaVer,
			&payload,
		)
		if err != nil {
			return nil, err
		}
		e.Payload = json.RawMessage(mustJSON(payload))
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) EventsAfter(
	ctx context.Context,
	stream string,
	after int64,
	limit int,
) ([]model.Event, error) {
	if limit <= 0 || limit > 2000 {
		limit = 500
	}
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id,stream,seq,type,ts,COALESCE(run_id,''),COALESCE(step_id,''),schema_ver,payload
		FROM events WHERE stream=? AND seq>? ORDER BY seq LIMIT ?`,
		stream,
		after,
		limit,
	)
	if err != nil {
		return nil, err
	}
	return scanEvents(rows)
}

func (s *Store) AllEventsAfter(ctx context.Context, after int64, limit int) ([]model.Event, error) {
	if limit <= 0 || limit > 2000 {
		limit = 500
	}
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id,stream,seq,type,ts,COALESCE(run_id,''),COALESCE(step_id,''),schema_ver,payload
		FROM events WHERE id>? ORDER BY id LIMIT ?`,
		after,
		limit,
	)
	if err != nil {
		return nil, err
	}
	return scanEvents(rows)
}

func (s *Store) RunEvents(
	ctx context.Context,
	runID string,
	after int64,
	limit int,
) ([]model.Event, error) {
	if limit <= 0 || limit > 2000 {
		limit = 500
	}
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id,stream,seq,type,ts,COALESCE(run_id,''),COALESCE(step_id,''),schema_ver,payload
		FROM events WHERE run_id=? AND id>? ORDER BY id LIMIT ?`,
		runID,
		after,
		limit,
	)
	if err != nil {
		return nil, err
	}
	return scanEvents(rows)
}

func (s *Store) ConversationEvents(
	ctx context.Context,
	convID string,
	after int64,
	limit int,
) ([]model.Event, error) {
	return s.EventsAfter(ctx, model.ConvStream(convID), after, limit)
}

func (s *Store) LastSeq(ctx context.Context, stream string) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0) FROM events WHERE stream=?`, stream).
		Scan(&n)
	return n, err
}

func (s *Store) EventCount(ctx context.Context) (int64, error) {
	var n int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}
