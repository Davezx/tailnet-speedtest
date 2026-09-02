package main

import (
	"bufio"
	"encoding/json"
	"os"
	"sync"
	"time"
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
	Retrans      int64     `json:"retrans"`    // downlink retransmitted TCP segments
	RetransPct   float64   `json:"retransPct"` // retrans / total downlink segments
	Direct       *bool     `json:"direct,omitempty"`
	Relay        string    `json:"relay,omitempty"`
}

// store keeps results as JSON Lines on disk plus an in-memory index.
// Volume is tiny (a manual test produces <1KB), so a full-file rewrite for
// note edits is fine and no database dependency is needed.
type store struct {
	path string
	mu   sync.Mutex
	rows []Result
}

func openStore(path string) (*store, error) {
	s := &store{path: path}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	var maxID int64
	for sc.Scan() {
		var r Result
		if json.Unmarshal(sc.Bytes(), &r) == nil && r.ID > 0 {
			s.rows = append(s.rows, r)
			if r.ID > maxID {
				maxID = r.ID
			}
		}
	}
	return s, sc.Err()
}

func (s *store) insert(r *Result) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var maxID int64
	for _, row := range s.rows {
		if row.ID > maxID {
			maxID = row.ID
		}
	}
	r.ID = maxID + 1
	line, err := json.Marshal(r)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	s.rows = append(s.rows, *r)
	return nil
}

// list returns newest-first results, capped at limit.
func (s *store) list(limit int) ([]Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Result{}
	for i := len(s.rows) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, s.rows[i])
	}
	return out, nil
}

func (s *store) setNote(id int64, note string) error {
	if len(note) > 200 {
		note = note[:200]
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	found := false
	for i := range s.rows {
		if s.rows[i].ID == id {
			s.rows[i].Note = note
			found = true
			break
		}
	}
	if !found {
		return nil
	}
	// rewrite whole file; tiny data set
	tmp := s.path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	for _, row := range s.rows {
		line, err := json.Marshal(row)
		if err != nil {
			f.Close()
			os.Remove(tmp)
			return err
		}
		w.Write(line)
		w.WriteByte('\n')
	}
	if err := w.Flush(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, s.path)
}
