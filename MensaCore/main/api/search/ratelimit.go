package search

import (
	"sync"
	"time"
)

var buckets sync.Map // user id -> *bucket

type bucket struct {
	mu    sync.Mutex
	count int
	reset time.Time
}

func allowReq(userID string) bool {
	now := time.Now()
	v, _ := buckets.LoadOrStore(userID, &bucket{reset: now.Add(time.Minute)})
	b := v.(*bucket)
	b.mu.Lock()
	defer b.mu.Unlock()
	if now.After(b.reset) {
		b.count = 0
		b.reset = now.Add(time.Minute)
	}
	if b.count >= 60 {
		return false
	}
	b.count++
	return true
}
