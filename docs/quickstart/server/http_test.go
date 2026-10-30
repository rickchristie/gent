package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/trace"
	"github.com/stretchr/testify/assert"
)

func TestHTTPStreaming(t *testing.T) {
	store := testStore(t, filepath.Join(t.TempDir(), "runs.db"))
	worker, cancel := context.WithCancel(context.Background())
	api := &API{store: store, worker: worker, running: map[string]*liveRun{},
		model: func() gent.Model { return &DemoModel{Delay: 40 * time.Millisecond} }}
	t.Cleanup(func() { cancel(); api.wait.Wait() })
	server := httptest.NewServer(api.routes(t.TempDir()))
	t.Cleanup(server.Close)
	response, err := http.Post(server.URL+"/api/runs", "application/json",
		strings.NewReader(`{"question":"Check order-1001"}`))
	if !assert.NoError(t, err) {
		return
	}
	assert.Equal(t, http.StatusAccepted, response.StatusCode)
	var created map[string]string
	assert.NoError(t, json.NewDecoder(response.Body).Decode(&created))
	assert.NoError(t, response.Body.Close())
	id := created["runId"]
	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/runs/" + id + "/events"
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if !assert.NoError(t, err) {
		return
	}
	defer func() { assert.NoError(t, conn.Close()) }()
	assert.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	var ready Frame
	assert.NoError(t, conn.ReadJSON(&ready))
	assert.Equal(t, Frame{Kind: "ready", RunID: id}, ready)
	response, err = http.Get(server.URL + "/api/runs/" + id + "/state")
	if !assert.NoError(t, err) {
		return
	}
	assert.Equal(t, "no-store", response.Header.Get("Cache-Control"))
	var snapshot struct {
		LastEventNumber string `json:"lastEventNumber"`
	}
	assert.NoError(t, json.NewDecoder(response.Body).Decode(&snapshot))
	assert.NoError(t, response.Body.Close())
	last, err := strconv.ParseUint(snapshot.LastEventNumber, 10, 64)
	assert.NoError(t, err)
	for {
		var frame Frame
		if !assert.NoError(t, conn.ReadJSON(&frame)) {
			return
		}
		if frame.Kind == "resync" {
			break
		}
		assert.Equal(t, id, frame.RunID)
		number, err := strconv.ParseUint(frame.EventNumber, 10, 64)
		assert.NoError(t, err)
		if number <= last {
			continue
		}
		assert.Equal(t, last+1, number)
		last = number
	}
	api.wait.Wait()
	final, err := api.snapshot(id)
	assert.NoError(t, err)
	assert.Equal(t, trace.RunStatusSucceeded, final.Status)
	if assert.Len(t, final.Answers, 1) {
		if assert.Len(t, final.Previews, 2) && assert.Len(t, final.Previews[1].Parts, 1) {
			assert.Equal(t, Answer{
				ContextID: id + ":root", MessageID: id + ":root:answer",
				SourceMessageIDs: []string{final.Previews[1].Parts[0].MessageId},
				Content:          []gent.ContentPart{gent.TextContent{Text: "Order order-1001 has shipped."}},
			}, final.Answers[0])
		}
	}
	assert.Equal(t, strconv.FormatUint(last, 10), final.LastEventNumber)
	if path := os.Getenv("GENT_TRACE_FIXTURE"); path != "" {
		writeFixture(t, store, id, path, final)
	}
}

func writeFixture(t *testing.T, store *Store, id, path string, final BrowserState) {
	t.Helper()
	saved, err := store.load(id)
	assert.NoError(t, err)
	seq, err := trace.RestoreSequencer(saved, displayConfig(store.codecs))
	assert.NoError(t, err)
	defer seq.Close()
	events, err := seq.EventsAfter(0)
	assert.NoError(t, err)
	frames := make([]Frame, 0, len(events))
	for _, event := range events {
		frames = append(frames, eventFrame(event))
	}
	initial := BrowserState{RunID: id, LastEventNumber: "0", Status: trace.RunStatusPending,
		Previews: []Preview{}, Answers: []Answer{}}
	data, err := json.MarshalIndent(map[string]any{
		"initial": initial, "frames": frames, "final": final,
	}, "", "  ")
	assert.NoError(t, err)
	assert.NoError(t, os.WriteFile(path, data, 0600))
}
