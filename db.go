package main

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// Result is one completed speed test. Metrics come from the browser;
// Identity, ClientIP, Direct, Relay and Retrans are enriched server-side.
type Result struct {
	ID           int64     `json:"id"`
	TS           time.Time `json:"ts"`
	Identity     string    `json:"identity"`
	ClientIP     string    `json:"clientIP"`
	Note         string    `json:"note"`
	DownSingle   float64   `json:"downSingle"` // Mbps
	DownMulti    float64   `json:"downMulti"`
	UpSingle     float64   `json:"upSingle"`
	UpMulti      float64   `json:"upMulti"`
	RTTIdleAvg   float64   `json:"rttIdleAvg"` // ms
	RTTIdleMin   float64   `json:"rttIdleMin"`
	RTTIdleMax   float64   `json:"rttIdleMax"`
	RTTLoadedAvg float64   `json:"rttLoadedAvg"`
	Jitter       float64   `json:"jitter"`
	Retrans      int64     `json:"retrans"`  // downlink retransmitted TCP segments
	RetransPct   float64   `json:"retransPct"` // retrans / total downlink segments
	Direct       *bool     `json:"direct,omitempty"`
	Relay        string    `json:"relay,omitempty"`
}

type store struct {
	db *sql.DB
}

func openStore(path string) (*store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS results (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		ts DATETIME NOT NULL,
		identity TEXT NOT NULL DEFAULT '',
		client_ip TEXT NOT NULL DEFAULT '',
		note TEXT NOT NULL DEFAULT '',
		down_single REAL, down_multi REAL, up_single REAL, up_multi REAL,
		rtt_idle_avg REAL, rtt_idle_min REAL, rtt_idle_max REAL, rtt_loaded_avg REAL,
		jitter REAL, retrans INTEGER, retrans_pct REAL,
		direct INTEGER, relay TEXT NOT NULL DEFAULT ''
	)`)
	if err != nil {
		return nil, fmt.Errorf("create table: %w", err)
	}
	return &store{db: db}, nil
}

func (s *store) insert(r *Result) error {
	var direct any
	if r.Direct != nil {
		direct = *r.Direct
	}
	res, err := s.db.Exec(`INSERT INTO results
		(ts, identity, client_ip, note, down_single, down_multi, up_single, up_multi,
		 rtt_idle_avg, rtt_idle_min, rtt_idle_max, rtt_loaded_avg, jitter,
		 retrans, retrans_pct, direct, relay)
		VALUES (?, ?, ?, '', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.TS, r.Identity, r.ClientIP, r.DownSingle, r.DownMulti, r.UpSingle, r.UpMulti,
		r.RTTIdleAvg, r.RTTIdleMin, r.RTTIdleMax, r.RTTLoadedAvg, r.Jitter,
		r.Retrans, r.RetransPct, direct, r.Relay)
	if err != nil {
		return err
	}
	r.ID, _ = res.LastInsertId()
	return nil
}

func (s *store) list(limit int) ([]Result, error) {
	rows, err := s.db.Query(`SELECT id, ts, identity, client_ip, note,
		down_single, down_multi, up_single, up_multi,
		rtt_idle_avg, rtt_idle_min, rtt_idle_max, rtt_loaded_avg, jitter,
		retrans, retrans_pct, direct, relay
		FROM results ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Result{}
	for rows.Next() {
		var r Result
		var direct sql.NullBool
		err := rows.Scan(&r.ID, &r.TS, &r.Identity, &r.ClientIP, &r.Note,
			&r.DownSingle, &r.DownMulti, &r.UpSingle, &r.UpMulti,
			&r.RTTIdleAvg, &r.RTTIdleMin, &r.RTTIdleMax, &r.RTTLoadedAvg, &r.Jitter,
			&r.Retrans, &r.RetransPct, &direct, &r.Relay)
		if err != nil {
			return nil, err
		}
		if direct.Valid {
			b := direct.Bool
			r.Direct = &b
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *store) setNote(id int64, note string) error {
	if len(note) > 200 {
		note = note[:200]
	}
	_, err := s.db.Exec(`UPDATE results SET note = ? WHERE id = ?`, note, id)
	return err
}
