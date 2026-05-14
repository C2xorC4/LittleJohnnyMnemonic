package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// sseBroker fans out string messages to all active SSE subscribers.
type sseBroker struct {
	mu      sync.Mutex
	clients map[chan string]struct{}
}

func newSSEBroker() *sseBroker {
	return &sseBroker{clients: map[chan string]struct{}{}}
}

func (b *sseBroker) subscribe() chan string {
	ch := make(chan string, 4)
	b.mu.Lock()
	b.clients[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *sseBroker) unsubscribe(ch chan string) {
	b.mu.Lock()
	delete(b.clients, ch)
	b.mu.Unlock()
	close(ch)
}

func (b *sseBroker) broadcast(msg string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.clients {
		select {
		case ch <- msg:
		default: // slow client — drop rather than block
		}
	}
}

func (b *sseBroker) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.clients)
}

// metricsWatcher polls a set of file paths for mtime changes.
type metricsWatcher struct {
	paths  []string
	mtimes map[string]time.Time
}

func newMetricsWatcher(metricsDir string) *metricsWatcher {
	watched := []string{
		filepath.Join(metricsDir, "recall_log.jsonl"),
		filepath.Join(metricsDir, "consolidation_outcomes.jsonl"),
		filepath.Join(metricsDir, "autodream_activation_snapshots.jsonl"),
		filepath.Join(metricsDir, "autodream_log.jsonl"),
	}
	w := &metricsWatcher{paths: watched, mtimes: map[string]time.Time{}}
	// Seed initial mtimes so first tick doesn't fire spuriously.
	for _, p := range watched {
		if info, err := os.Stat(p); err == nil {
			w.mtimes[p] = info.ModTime()
		}
	}
	return w
}

// changed returns true if any watched file has a newer mtime than last seen.
func (w *metricsWatcher) changed() bool {
	dirty := false
	for _, p := range w.paths {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		if !info.ModTime().Equal(w.mtimes[p]) {
			w.mtimes[p] = info.ModTime()
			dirty = true
		}
	}
	return dirty
}

func cmdMetricsServe(vaultRoot string, args []string) {
	fs := flag.NewFlagSet("metrics serve", flag.ExitOnError)
	port := fs.Int("port", 8080, "HTTP port to listen on")
	pollInterval := fs.Duration("poll", 2*time.Second, "file-change poll interval")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: jm metrics serve [flags]")
		fmt.Fprintln(fs.Output(), "")
		fmt.Fprintln(fs.Output(), "Serve a live metrics dashboard over HTTP.")
		fmt.Fprintln(fs.Output(), "The page reflects current log data on every load.")
		fmt.Fprintln(fs.Output(), "Connected browsers receive push updates via SSE when metrics files change.")
		fmt.Fprintln(fs.Output(), "")
		fmt.Fprintln(fs.Output(), "Flags:")
		fs.PrintDefaults()
	}
	_ = fs.Parse(args)

	metricsDir := filepath.Join(vaultRoot, "Metrics")
	title := "LJM Metrics — " + filepath.Base(vaultRoot)

	broker := newSSEBroker()
	watcher := newMetricsWatcher(metricsDir)

	// File-change watcher goroutine — polls and broadcasts JSON payload on change.
	go func() {
		ticker := time.NewTicker(*pollInterval)
		defer ticker.Stop()
		for range ticker.C {
			if !watcher.changed() {
				continue
			}
			payload, err := buildDashboardPayload(metricsDir)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[metrics serve] reload: %v\n", err)
				continue
			}
			payload.Meta["generated_at"] = time.Now().UTC().Format(time.RFC3339)
			data, err := json.Marshal(payload)
			if err != nil {
				continue
			}
			broker.broadcast(string(data))
			fmt.Printf("[metrics serve] pushed update (%d client(s))\n", broker.count())
		}
	}()

	mux := http.NewServeMux()

	// GET / — serve fresh dashboard HTML on every request.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		payload, err := buildDashboardPayload(metricsDir)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		payload.Meta["generated_at"] = time.Now().UTC().Format(time.RFC3339)
		rendered, err := renderDashboardHTML(payload, title, "live")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(rendered)
	})

	// GET /api/data — current payload as JSON (for manual fetch / debugging).
	mux.HandleFunc("/api/data", func(w http.ResponseWriter, r *http.Request) {
		payload, err := buildDashboardPayload(metricsDir)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		payload.Meta["generated_at"] = time.Now().UTC().Format(time.RFC3339)
		data, err := json.Marshal(payload)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(data)
	})

	// GET /events — SSE stream; pushes JSON payload to client on file changes.
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no") // nginx pass-through

		ch := broker.subscribe()
		defer broker.unsubscribe(ch)

		// Keepalive comment every 15 s so proxies don't drop idle connections.
		keepalive := time.NewTicker(15 * time.Second)
		defer keepalive.Stop()

		fmt.Fprintf(w, ": connected\n\n")
		flusher.Flush()

		for {
			select {
			case msg, ok := <-ch:
				if !ok {
					return
				}
				fmt.Fprintf(w, "data: %s\n\n", msg)
				flusher.Flush()
			case <-keepalive.C:
				fmt.Fprintf(w, ": keepalive\n\n")
				flusher.Flush()
			case <-r.Context().Done():
				return
			}
		}
	})

	addr := fmt.Sprintf(":%d", *port)
	fmt.Printf("LJM Metrics  →  http://localhost:%d\n", *port)
	fmt.Printf("Watching %s  (poll %s)\n", metricsDir, *pollInterval)
	fmt.Println("Ctrl+C to stop")

	if err := http.ListenAndServe(addr, mux); err != nil {
		fmt.Fprintf(os.Stderr, "metrics serve: %v\n", err)
		os.Exit(1)
	}
}
