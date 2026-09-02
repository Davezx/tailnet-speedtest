package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

type wsMessage struct {
	Type string `json:"type"`          // "ping" | "done"
	Seq  int    `json:"seq,omitempty"` // client sequence number (ping)
	TS   int64  `json:"ts,omitempty"`  // client send timestamp, ms since epoch (echoed back)
	Sent int    `json:"sent,omitempty"`// total pings the client sent (done)
}

type wsResult struct {
	Type     string `json:"type"` // "result"
	Received int    `json:"received"`
	Missing  []int  `json:"missing,omitempty"`
}

// handleWS echoes ping messages for RTT/jitter measurement and tracks which
// sequence numbers arrived so the client can compute uplink packet loss.
// Downlink loss is computed client-side from echoes that never come back.
func (s *server) handleWS(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// This is a tailnet-internal tool; origin checks add nothing here.
		InsecureSkipVerify: true,
	})
	if err != nil {
		log.Printf("ws accept: %v", err)
		return
	}
	defer c.Close(websocket.StatusInternalError, "")

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()

	seen := make(map[int]bool)
	received := 0
	for {
		var msg wsMessage
		if err := wsjson.Read(ctx, c, &msg); err != nil {
			return // client closed or timed out
		}
		switch msg.Type {
		case "ping":
			if !seen[msg.Seq] {
				seen[msg.Seq] = true
				received++
			}
			if err := wsjson.Write(ctx, c, msg); err != nil {
				return
			}
		case "done":
			missing := []int{}
			for i := 0; i < msg.Sent; i++ {
				if !seen[i] {
					missing = append(missing, i)
				}
			}
			wsjson.Write(ctx, c, wsResult{Type: "result", Received: received, Missing: missing})
			c.Close(websocket.StatusNormalClosure, "done")
			return
		}
	}
}
