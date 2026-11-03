package tokencenter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestChaosWorkerUsesPublishedCadenceChangesAndRetainsItOnFailure(t *testing.T) {
	var cycles atomic.Int32
	starts := make(chan time.Time, 8)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := 1
		if r.URL.Query().Get("cursor") != "" {
			page = 2
		} else {
			cycles.Add(1)
			starts <- time.Now()
		}
		cycle := cycles.Load()
		if cycle == 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		seconds := 1
		if cycle == 2 {
			seconds = 2
		}
		root := chaosFixturePage(t, page)
		root["version"] = fmt.Sprintf("51:cadence-%d", seconds)
		root["config"].(map[string]any)["delivery"].(map[string]any)["pullIntervalSeconds"] = seconds
		json.NewEncoder(w).Encode(root)
	}))
	defer server.Close()
	s := chaosService(t, &memoryStore{}, server)
	s.cfg.Interval = time.Hour
	s.Start(context.Background())
	defer s.Stop()
	next := func() time.Time {
		t.Helper()
		select {
		case at := <-starts:
			return at
		case <-time.After(3500 * time.Millisecond):
			t.Fatal("worker ignored successful central pullIntervalSeconds or reverted to local fallback")
			return time.Time{}
		}
	}
	first, second, third, fourth, fifth := next(), next(), next(), next(), next()
	for _, step := range []struct {
		name              string
		elapsed, expected time.Duration
	}{
		{"first published interval", second.Sub(first), time.Second},
		{"updated published interval", third.Sub(second), 2 * time.Second},
		{"retained interval after failed sync", fourth.Sub(third), 2 * time.Second},
		{"later shortened interval", fifth.Sub(fourth), time.Second},
	} {
		if step.elapsed < step.expected-150*time.Millisecond || step.elapsed > step.expected+1200*time.Millisecond {
			t.Fatalf("%s changed: elapsed=%v expected=%v", step.name, step.elapsed, step.expected)
		}
	}
	s.Stop()
}
