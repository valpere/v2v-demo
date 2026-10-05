package main

import (
	"sync"
	"time"
)

// admission is the limiter's verdict for one incoming message.
type admission int

const (
	admitOK             admission = iota // process the turn
	admitThrottleNotify                  // rate-limited: drop it, tell the sender once
	admitDayCapNotify                    // daily cap hit: drop it, tell the sender once today
	admitDrop                            // drop silently (already told)
)

// limiter bounds what one chat — and the whole bot — can spend: a per-chat
// token bucket (messages/min), a per-chat daily turn count and a global daily
// count of paid turns. Zero values disable the matching limit. Daily counters
// reset at the local midnight of loc. Safe for concurrent use.
type limiter struct {
	mu  sync.Mutex
	now func() time.Time
	loc *time.Location

	burst, perMin        int
	chatDaily, globalCap int
	exempt               map[int64]bool

	buckets  map[int64]*bucket
	day      string
	chatDay  map[int64]int
	global   int
	dayNoted map[int64]bool // chats already told about a daily cap today
}

type bucket struct {
	tokens float64
	last   time.Time
	noted  bool // throttle notice already sent since the last refill
}

func newLimiter(cfg Config) *limiter {
	l := &limiter{
		now:       time.Now,
		loc:       time.UTC,
		burst:     cfg.RateBurst,
		perMin:    cfg.RatePerMin,
		chatDaily: cfg.ChatDailyTurns,
		globalCap: cfg.DailyTurns,
		exempt:    map[int64]bool{},
		buckets:   map[int64]*bucket{},
	}
	for _, id := range cfg.RateExemptChats {
		l.exempt[id] = true
	}
	return l
}

func (l *limiter) admit(chatID int64) admission {
	if l.exempt[chatID] {
		return admitOK
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()

	if d := now.In(l.loc).Format("2006-01-02"); d != l.day {
		l.day, l.chatDay, l.global, l.dayNoted = d, map[int64]int{}, 0, map[int64]bool{}
	}

	if l.burst > 0 && l.perMin > 0 {
		b := l.buckets[chatID]
		if b == nil {
			b = &bucket{tokens: float64(l.burst), last: now}
			l.buckets[chatID] = b
		}
		b.tokens += now.Sub(b.last).Seconds() * float64(l.perMin) / 60
		if b.tokens > float64(l.burst) {
			b.tokens = float64(l.burst)
		}
		b.last = now
		if b.tokens < 1 {
			if b.noted {
				return admitDrop
			}
			b.noted = true
			return admitThrottleNotify
		}
		b.tokens--
		b.noted = false
	}

	if (l.chatDaily > 0 && l.chatDay[chatID] >= l.chatDaily) || (l.globalCap > 0 && l.global >= l.globalCap) {
		if l.dayNoted[chatID] {
			return admitDrop
		}
		l.dayNoted[chatID] = true
		return admitDayCapNotify
	}
	l.chatDay[chatID]++
	l.global++
	return admitOK
}
