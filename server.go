package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"time"
)

type server struct {
	maxDownload int64
	randBuf     []byte // 1 MiB of random data, cycled for download payloads
}

func newServer(maxDownload int64) *server {
	buf := make([]byte, 1<<20)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	return &server{maxDownload: maxDownload, randBuf: buf}
}

// handleDownload streams size bytes of random data. Random (not zeros) so any
// compressing middlebox can't distort the measurement.
func (s *server) handleDownload(w http.ResponseWriter, r *http.Request) {
	size, err := strconv.ParseInt(r.URL.Query().Get("size"), 10, 64)
	if err != nil || size <= 0 {
		http.Error(w, "bad size", http.StatusBadRequest)
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
			return // client went away
		}
		remaining -= n
		if canFlush {
			flusher.Flush()
		}
	}
}

// handleUpload discards the request body and reports how many bytes arrived.
func (s *server) handleUpload(w http.ResponseWriter, r *http.Request) {
	n, err := io.Copy(io.Discard, r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int64{"received": n})
}

type infoResponse struct {
	ServerTime time.Time      `json:"serverTime"`
	ClientIP   string         `json:"clientIP"`
	Tailscale  *tailscaleInfo `json:"tailscale,omitempty"`
}

type tailscaleInfo struct {
	PeerName string `json:"peerName,omitempty"`
	Direct   *bool  `json:"direct,omitempty"` // true=direct path, false=via DERP relay
	Relay    string `json:"relay,omitempty"`
}

// handleInfo reports request metadata and, best-effort, how this client's
// traffic reaches the server over the tailnet (direct vs DERP relay).
func (s *server) handleInfo(w http.ResponseWriter, r *http.Request) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	resp := infoResponse{ServerTime: time.Now(), ClientIP: host}
	resp.Tailscale = lookupTailscalePeer(host)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// lookupTailscalePeer asks the local tailscaled about a peer IP. Returns nil
// when tailscale is unavailable or the IP is not a tailnet peer.
func lookupTailscalePeer(ip string) *tailscaleInfo {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	type peerStatus struct {
		HostName string
		DNSName  string
		TailscaleIPs []string
		CurAddr  string // non-empty => direct path
		Relay    string
	}
	var status struct {
		Peer map[string]peerStatus
	}
	cmd := exec.CommandContext(ctx, "tailscale", "status", "--json")
	out, err := cmd.Output()
	if err != nil || json.Unmarshal(out, &status) != nil {
		return nil
	}
	for _, p := range status.Peer {
		for _, pIP := range p.TailscaleIPs {
			if pIP != ip {
				continue
			}
			direct := p.CurAddr != ""
			name := p.DNSName
			if name == "" {
				name = p.HostName
			}
			return &tailscaleInfo{PeerName: name, Direct: &direct, Relay: p.Relay}
		}
	}
	return nil
}
