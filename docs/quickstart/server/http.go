package main

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/trace"
)

func (a *API) routes(uiPath string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/runs", a.createRun)
	mux.HandleFunc("GET /api/runs/{id}/state", a.getState)
	mux.HandleFunc("GET /api/runs/{id}/events", a.streamEvents)
	mux.HandleFunc("POST /api/runs/{id}/resume", a.resumeRun)
	mux.Handle("/", http.FileServer(http.Dir(uiPath)))
	return mux
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		log.Printf("encode HTTP response: %v", err)
		http.Error(w, "response encoding failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if _, err := w.Write(data); err != nil {
		log.Printf("write HTTP response: %v", err)
	}
}

func writeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, errMissing) {
		status = http.StatusNotFound
	}
	if errors.Is(err, errBusy) {
		status = http.StatusConflict
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func (a *API) createRun(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Question string `json:"question"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if strings.TrimSpace(body.Question) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "question is required"})
		return
	}
	id := rand.Text()
	if err := a.start(id, body.Question, false); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"runId": id})
}

func (a *API) resumeRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := a.start(id, "", true); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"runId": id})
}

func (a *API) getState(w http.ResponseWriter, r *http.Request) {
	state, err := a.snapshot(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func socketWrite(conn *websocket.Conn, value any) error {
	if err := conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	return conn.WriteJSON(value)
}

func (a *API) streamEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	live := a.active(id)
	var seq *trace.Sequencer
	if live != nil {
		seq = live.seq
	} else {
		saved, err := a.store.load(id)
		if err != nil {
			writeError(w, err)
			return
		}
		seq, err = trace.RestoreSequencer(saved, displayConfig(a.store.codecs))
		if err != nil {
			writeError(w, err)
			return
		}
		defer seq.Close()
	}
	// Install the bounded subscription before sending ready. A slow socket cannot block Save.
	subscription, err := seq.SubscribeWithLimits(gent.DeliveryConfig{
		MaxEvents: 256, MaxBytes: 2 * 1024 * 1024, Timeout: 5 * time.Second,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	defer subscription.Close()
	updates := subscription.Events()
	// Include the unsaved prefix that was already projected before this subscription existed.
	// The frontend deduplicates overlap with its saved snapshot and with queued live events.
	saved, err := a.store.load(id)
	if err != nil {
		writeError(w, err)
		return
	}
	backlog, err := seq.EventsAfter(saved.LastAppliedEventNumber)
	if err != nil {
		writeError(w, err)
		return
	}
	upgrader := websocket.Upgrader{} // The default checks that Origin matches the request host.
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("upgrade WebSocket: %v", err)
		return
	}
	defer func() {
		if err := conn.Close(); err != nil {
			log.Printf("close WebSocket: %v", err)
		}
	}()
	conn.SetReadLimit(1024)
	if err := conn.SetReadDeadline(time.Now().Add(20 * time.Second)); err != nil {
		log.Printf("set read deadline: %v", err)
		return
	}
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(20 * time.Second))
	})
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				log.Printf("WebSocket reader stopped: %v", err)
				return
			}
		}
	}()
	if err := socketWrite(conn, Frame{Kind: "ready", RunID: id}); err != nil {
		log.Printf("write ready: %v", err)
		return
	}
	for _, event := range backlog {
		if err := socketWrite(conn, eventFrame(event)); err != nil {
			log.Printf("write buffered event: %v", err)
			return
		}
	}
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-a.worker.Done():
			return
		case <-closed:
			return
		case event, ok := <-updates:
			if !ok {
				if err := subscription.Err(); err != nil {
					log.Printf("live subscription stopped: %v", err)
				}
				if err := socketWrite(conn, Frame{Kind: "resync", RunID: id}); err != nil {
					log.Printf("write resync: %v", err)
				}
				return
			}
			if err := socketWrite(conn, eventFrame(event)); err != nil {
				log.Printf("write event: %v", err)
				return
			}
		case <-tick.C:
			// A worker can start after an idle client joins. Reconnect to the new sequencer.
			if a.active(id) != live {
				return
			}
			if err := conn.WriteControl(websocket.PingMessage, nil,
				time.Now().Add(5*time.Second)); err != nil {
				log.Printf("write ping: %v", err)
				return
			}
			if err := socketWrite(conn, Frame{Kind: "heartbeat", RunID: id}); err != nil {
				log.Printf("write heartbeat: %v", err)
				return
			}
		}
	}
}
