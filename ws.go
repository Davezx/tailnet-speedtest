package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

type wsPing struct {
	Seq int   `json:"seq"`
	TS  int64 `json:"ts"` // client send timestamp, echoed back
}

// handleWS echoes ping messages for the whole duration of a test. The client
// pings rapidly while idle (baseline RTT/jitter) and keeps pinging during the
// bandwidth phases (latency under load).
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

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	for {
		var msg wsPing
		if err := wsjson.Read(ctx, c, &msg); err != nil {
			return // client closed or timed out
		}
		if err := wsjson.Write(ctx, c, msg); err != nil {
			return
		}
	}
}
