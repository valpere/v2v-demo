package main

import (
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }

func newTestLimiter(cfg Config) (*limiter, *fakeClock) {
	c := &fakeClock{t: time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)}
	l := newLimiter(cfg)
	l.now = c.now
	return l, c
}

func TestLimiterBurstThenRefill(t *testing.T) {
	l, clk := newTestLimiter(Config{RateBurst: 4, RatePerMin: 6}) // 1 token / 10 s
	for i := 0; i < 4; i++ {
		if d := l.admit(1); d != admitOK {
			t.Fatalf("burst msg %d: %v", i, d)
		}
	}
	if d := l.admit(1); d != admitThrottleNotify {
		t.Fatalf("5th: want one notice, got %v", d)
	}
	if d := l.admit(1); d != admitDrop {
		t.Fatalf("6th: want silent drop, got %v", d)
	}
	if d := l.admit(2); d != admitOK { // another chat is unaffected
		t.Fatalf("chat 2: %v", d)
	}
	clk.t = clk.t.Add(11 * time.Second)
	if d := l.admit(1); d != admitOK {
		t.Fatalf("after refill: %v", d)
	}
	// the notice is armed again once the bucket has refilled
	for l.admit(1) == admitOK {
	}
	clk.t = clk.t.Add(11 * time.Second)
	l.admit(1)
	if d := l.admit(1); d != admitThrottleNotify {
		t.Fatalf("notice should re-arm after a refill, got %v", d)
	}
}

func TestLimiterChatDailyCapResets(t *testing.T) {
	l, clk := newTestLimiter(Config{ChatDailyTurns: 2})
	l.admit(1)
	l.admit(1)
	if d := l.admit(1); d != admitDayCapNotify {
		t.Fatalf("want day-cap notice, got %v", d)
	}
	if d := l.admit(1); d != admitDrop {
		t.Fatalf("notice only once per day, got %v", d)
	}
	if d := l.admit(2); d != admitOK {
		t.Fatalf("other chat unaffected: %v", d)
	}
	clk.t = clk.t.Add(24 * time.Hour)
	if d := l.admit(1); d != admitOK {
		t.Fatalf("next day resets: %v", d)
	}
}

func TestLimiterGlobalDailyCap(t *testing.T) {
	l, clk := newTestLimiter(Config{DailyTurns: 3})
	l.admit(1)
	l.admit(2)
	l.admit(3)
	if d := l.admit(4); d != admitDayCapNotify {
		t.Fatalf("want global-cap notice for chat 4, got %v", d)
	}
	if d := l.admit(4); d != admitDrop {
		t.Fatalf("one notice per chat per day, got %v", d)
	}
	if d := l.admit(5); d != admitDayCapNotify {
		t.Fatalf("each chat gets its own notice, got %v", d)
	}
	clk.t = clk.t.Add(24 * time.Hour)
	if d := l.admit(4); d != admitOK {
		t.Fatalf("next day resets: %v", d)
	}
}

func TestLimiterExemptAndDisabled(t *testing.T) {
	l, _ := newTestLimiter(Config{RateBurst: 1, RatePerMin: 1, DailyTurns: 1, RateExemptChats: []int64{9}})
	for i := 0; i < 20; i++ {
		if d := l.admit(9); d != admitOK {
			t.Fatalf("exempt chat throttled at %d: %v", i, d)
		}
	}
	off, _ := newTestLimiter(Config{}) // all zero = no limits
	for i := 0; i < 500; i++ {
		if d := off.admit(1); d != admitOK {
			t.Fatalf("disabled limiter refused at %d: %v", i, d)
		}
	}
}
