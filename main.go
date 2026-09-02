// Command tailnet-speedtest serves a browser-based speed test for tailnet links:
// download/upload throughput, RTT, jitter and packet-loss estimation.
package main

import (
	"embed"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"time"
)

//go:embed static/index.html
var staticFS embed.FS

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	maxDownload := flag.Int64("max-download", 512<<20, "max bytes allowed per download request")
	flag.Parse()

	s := newServer(*maxDownload)
	staticSub, err := fs.Sub(staticFS, "static")
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServerFS(staticSub))
	mux.HandleFunc("GET /api/download", s.handleDownload)
	mux.HandleFunc("POST /api/upload", s.handleUpload)
	mux.HandleFunc("GET /api/info", s.handleInfo)
	mux.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /ws", s.handleWS)

	srv := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("tailnet-speedtest listening on %s", *addr)
	log.Fatal(srv.ListenAndServe())
}
