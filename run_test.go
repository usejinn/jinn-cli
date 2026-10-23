package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// stdout runs f and returns what it printed.
func stdout(t *testing.T, f func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	real := os.Stdout
	os.Stdout = w
	ferr := f()
	os.Stdout = real
	w.Close()
	b, _ := io.ReadAll(r)
	return string(b), ferr
}

// A start that repeats an Idempotency-Key returns the first run as it is now.
// jinn run says that run's state, not "queued", and a run without an agent
// that someone already finished is a success, not a failure.
func TestRunAReplayedRun(t *testing.T) {
	const body = `{"id":"run_1","function":"fnc_1","version":3,"agent":false,"state":"succeeded","ready_at":"2026-10-23T10:00:00Z"}`
	var key string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && r.URL.Path == "/v1/functions/fnc_1/runs":
			key = r.Header.Get("Idempotency-Key")
			w.Header().Set("Idempotent-Replayed", "true")
			w.WriteHeader(http.StatusCreated)
			io.WriteString(w, body)
		case r.Method == "GET" && r.URL.Path == "/v1/runs/run_1":
			io.WriteString(w, body)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	t.Setenv("JINN_API", srv.URL)
	t.Setenv("JINN_KEY", "jinn_test")
	ctx := context.Background()
	out, err := stdout(t, func() error { return run(ctx, []string{"fnc_1", "--idempotency-key", "evt_1", "--detach"}) })
	if err != nil || key != "evt_1" || !strings.HasPrefix(out, "run_1 succeeded · fnc_1 v3") {
		t.Fatalf("detach: %q, %v (key %q)", out, err, key)
	}
	out, err = stdout(t, func() error { return run(ctx, []string{"fnc_1", "--idempotency-key", "evt_1"}) })
	if err != nil || !strings.Contains(out, "run_1 succeeded\n") {
		t.Fatalf("follow: %q, %v", out, err)
	}
}
