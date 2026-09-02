// Command tailnet-speedtest serves a browser-based speed test for tailnet links:
// single/multi-stream download/upload throughput, idle vs loaded RTT, jitter,
// downlink TCP retransmits, with server-side SQLite history.
package main

import (
	"context"
	"embed"
	"flag"
	"io/fs"
	"log"
	"net"
	"net/http"
	"time"
)

//go:embed static/index.html
var staticFS embed.FS

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	maxDownload := flag.Int64("max-download", 512<<20, "max bytes allowed per download request")
	dbPath := flag.String("db", "speedtest.jsonl", "results storage path (JSON Lines)")
	flag.Parse()

	st, err := openStore(*dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	s := newServer(*maxDownload, st)

	staticSub, err := fs.Sub(staticFS, "static")
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServerFS(staticSub))
	mux.HandleFunc("GET /api/download", s.handleDownload)
	mux.HandleFunc("POST /api/upload", s.handleUpload)
	mux.HandleFunc("GET /api/info", s.handleInfo)
	mux.HandleFunc("POST /api/test/begin", s.handleBegin)
	mux.HandleFunc("POST /api/test/end", s.handleEnd)
	mux.HandleFunc("POST /api/results", s.handleSubmitResult)
	mux.HandleFunc("GET /api/results", s.handleListResults)
	mux.HandleFunc("POST /api/results/{id}/note", s.handleSetNote)
	mux.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /ws", s.handleWS)

	srv := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		// Expose the connection to handlers so the download handler can read
		// TCP_INFO (retransmit count) after streaming.
		ConnContext: func(ctx context.Context, c net.Conn) context.Context {
			return context.WithValue(ctx, connCtxKey{}, c)
		},
	}
	log.Printf("tailnet-speedtest listening on %s (db: %s)", *addr, *dbPath)
	log.Fatal(srv.ListenAndServe())
}
