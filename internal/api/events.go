package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"myagent/internal/model"
)

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	stream := r.URL.Query().Get("stream")
	if stream == "" {
		stream = model.StreamSys
	}
	after := parseInt64(r.URL.Query().Get("after"), 0)
	if after == 0 {
		if v := r.Header.Get("Last-Event-ID"); v != "" {
			after = parseInt64(v, 0)
		}
	}
	// live=1 starts at the current tail. It is used by screens that already
	// loaded persisted state through the REST API and only need new events.
	liveOnly := r.URL.Query().Get("live") == "1"
	if liveOnly && after == 0 {
		after, _ = s.app.Store.LastSeq(r.Context(), stream)
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming is not supported", nil)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	sub := s.app.Bus.Subscribe(stream)
	defer sub.Close()
	last := after
	// Replay first, while the live subscription is already buffering new events.
	if !liveOnly {
		for {
			events, err := s.app.Store.EventsAfter(r.Context(), stream, last, 500)
			if err != nil {
				break
			}
			for _, e := range events {
				if e.Seq <= last {
					continue
				}
				if err := writeSSE(w, e); err != nil {
					return
				}
				last = e.Seq
			}
			if len(events) < 500 {
				break
			}
		}
	}
	flusher.Flush()

	ticker := time.NewTicker(18 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case evt, ok := <-sub.Ch:
			if !ok {
				return
			}
			if evt.Seq > 0 && evt.Seq <= last {
				continue
			}
			if evt.Seq > 0 {
				if evt.Seq > last+1 && last > 0 {
					// A gap means the server dropped this subscriber. Closing
					// makes EventSource reconnect with Last-Event-ID.
					return
				}
				last = evt.Seq
			}
			if err := writeSSE(w, evt); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func writeSSE(w http.ResponseWriter, e model.Event) error {
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	var b strings.Builder
	if e.Seq > 0 {
		fmt.Fprintf(&b, "id: %d\n", e.Seq)
	}
	fmt.Fprintf(&b, "event: %s\n", e.Type)
	fmt.Fprintf(&b, "data: %s\n\n", raw)
	_, err = fmt.Fprint(w, b.String())
	return err
}

func (s *Server) handleEventsReplay(w http.ResponseWriter, r *http.Request) {
	stream := r.URL.Query().Get("stream")
	if stream == "" {
		stream = model.StreamSys
	}
	after := parseInt64(r.URL.Query().Get("after"), 0)
	limit := int(parseInt64(r.URL.Query().Get("limit"), 500))
	events, err := s.app.Store.EventsAfter(r.Context(), stream, after, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), nil)
		return
	}
	last := after
	if len(events) > 0 {
		last = events[len(events)-1].Seq
	}
	writeJSON(w, http.StatusOK, map[string]any{"stream": stream, "events": events, "last_seq": last})
}

func parseInt64(s string, def int64) int64 {
	if s == "" {
		return def
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return def
	}
	return n
}
