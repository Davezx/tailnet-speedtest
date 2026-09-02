package main

import (
	"sync"
	"time"
)

// testLimiter enforces: at most one test running globally (parallel tests
// would corrupt each other's bandwidth numbers), and at most maxPerMin
// test starts per client IP per minute.
type testLimiter struct {
	mu         sync.Mutex
	holder     string
	expires    time.Time
	perIP      map[string][]time.Time
	maxPerMin  int
	maxRunTime time.Duration
}

func newTestLimiter() *testLimiter {
	return &testLimiter{
		perIP:      map[string][]time.Time{},
		maxPerMin:  3,
		maxRunTime: 3 * time.Minute,
	}
}

// begin returns "" on success, or a user-facing error message.
func (l *testLimiter) begin(ip string) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if now.Before(l.expires) && l.holder != ip {
		return "正有其他设备的测试在进行，请稍后再试"
	}
	// prune and count this IP's starts in the last minute
	kept := l.perIP[ip][:0]
	for _, t := range l.perIP[ip] {
		if now.Sub(t) < time.Minute {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.maxPerMin {
		return "测试过于频繁，每分钟最多 3 次，请稍后再试"
	}
	l.perIP[ip] = append(kept, now)
	l.holder = ip
	l.expires = now.Add(l.maxRunTime)
	return ""
}

func (l *testLimiter) end(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.holder == ip {
		l.holder = ""
		l.expires = time.Time{}
	}
}
