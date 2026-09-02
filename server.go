package main

import (
	"crypto/rand"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

type connCtxKey struct{}

type tailscaleInfo struct {
	PeerName string `json:"peerName,omitempty"`
	Direct   *bool  `json:"direct,omitempty"` // true=direct path, false=via DERP relay
	Relay    string `json:"relay,omitempty"`
}

// retransEntry accumulates downlink TCP retransmits per client IP, read from
// each download connection via TCP_INFO. Consumed when the client submits its
// result; entries older than maxRunTime are stale.
type retransEntry struct {
	segs  int64
	bytes int64
	at    time.Time
}

type server struct {
	maxDownload int64
	randBuf     []byte // 1 MiB of random data, cycled for download payloads
	store       *store
	limiter     *testLimiter

	retransMu sync.Mutex
	retrans   map[string]retransEntry
}

func newServer(maxDownload int64, st *store) *server {
	buf := make([]byte, 1<<20)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	return &server{
		maxDownload: maxDownload,
		randBuf:     buf,
		store:       st,
		limiter:     newTestLimiter(),
		retrans:     map[string]retransEntry{},
	}
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// ---- test slot management --------------------------------------------------

func (s *server) handleBegin(w http.ResponseWriter, r *http.Request) {
	if msg := s.limiter.begin(clientIP(r)); msg != "" {
		writeErr(w, http.StatusTooManyRequests, msg)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *server) handleEnd(w http.ResponseWriter, r *http.Request) {
	s.limiter.end(clientIP(r))
	writeJSON(w, map[string]string{"status": "ok"})
}

// ---- throughput endpoints --------------------------------------------------

// handleDownload streams size bytes of random data. Random (not zeros) so any
// compressing middlebox can't distort the measurement. After streaming, the
// connection's TCP_INFO retransmit count is recorded for the result submit.
func (s *server) handleDownload(w http.ResponseWriter, r *http.Request) {
	size, err := strconv.ParseInt(r.URL.Query().Get("size"), 10, 64)
	if err != nil || size <= 0 {
		writeErr(w, http.StatusBadRequest, "bad size")
		return
	}
	if size > s.maxDownload {
		size = s.maxDownload
	}

	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Length", strconv.FormatInt(size, 10))

	flusher, canFlush := w.(http.Flusher)
	remaining := size
	for remaining > 0 {
		n := int64(len(s.randBuf))
		if remaining < n {
			n = remaining
		}
		if _, err := w.Write(s.randBuf[:n]); err != nil {
			return // client went away; nothing meaningful to record
		}
		remaining -= n
		if canFlush {
			flusher.Flush()
		}
	}
	s.recordRetrans(r, size-remaining)
}

// recordRetrans reads TCP_INFO off the request's connection and accumulates
// the retransmitted-segment count for this client.
func (s *server) recordRetrans(r *http.Request, sent int64) {
	conn, ok := r.Context().Value(connCtxKey{}).(syscall.Conn)
	if !ok {
		return
	}
	raw, err := conn.SyscallConn()
	if err != nil {
		return
	}
	var retrans int64
	if err := raw.Control(func(fd uintptr) {
		info, err := unix.GetsockoptTCPInfo(int(fd), unix.SOL_TCP, unix.TCP_INFO)
		if err == nil {
			retrans = int64(info.Total_retrans)
		}
	}); err != nil {
		return
	}
	s.retransMu.Lock()
	e := s.retrans[clientIP(r)]
	e.segs += retrans
	e.bytes += sent
	e.at = time.Now()
	s.retrans[clientIP(r)] = e
	s.retransMu.Unlock()
}

// handleUpload discards the request body and reports how many bytes arrived.
func (s *server) handleUpload(w http.ResponseWriter, r *http.Request) {
	n, err := io.Copy(io.Discard, r.Body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, map[string]int64{"received": n})
}

// ---- results ---------------------------------------------------------------

// handleSubmitResult stores a finished test. The browser supplies the metrics
// it measured; the server enriches with whois identity, direct/relay path and
// the downlink retransmit count collected during the download phases.
func (s *server) handleSubmitResult(w http.ResponseWriter, r *http.Request) {
	var res Result
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&res); err != nil {
		writeErr(w, http.StatusBadRequest, "bad result payload")
		return
	}
	ip := clientIP(r)
	res.ID = 0
	res.TS = time.Now()
	res.ClientIP = ip
	if id := whoisIdentity(ip); id != "" {
		res.Identity = id
	} else {
		res.Identity = ip
	}
	if ts := lookupTailscalePeer(ip); ts != nil {
		res.Direct = ts.Direct
		res.Relay = ts.Relay
	}

	s.retransMu.Lock()
	if e, ok := s.retrans[ip]; ok && time.Since(e.at) < s.limiter.maxRunTime {
		res.Retrans = e.segs
		if e.bytes > 0 {
			// approximate total segments assuming ~1460B MSS
			res.RetransPct = float64(e.segs) / (float64(e.bytes) / 1460) * 100
		}
		delete(s.retrans, ip)
	}
	s.retransMu.Unlock()

	if err := s.store.insert(&res); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, res)
}

func (s *server) handleListResults(w http.ResponseWriter, r *http.Request) {
	limit := 500
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 5000 {
		limit = v
	}
	results, err := s.store.list(limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, results)
}

func (s *server) handleSetNote(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	var body struct {
		Note string `json:"note"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad payload")
		return
	}
	if err := s.store.setNote(id, body.Note); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

// ---- misc ------------------------------------------------------------------

func (s *server) handleInfo(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	writeJSON(w, map[string]any{
		"serverTime": time.Now(),
		"clientIP":   ip,
		"identity":   whoisIdentity(ip),
		"tailscale":  lookupTailscalePeer(ip),
	})
}
