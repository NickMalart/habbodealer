package main

import (
	"sync"
	"time"
)

type BufferedMsg struct {
	Text string
	At   time.Time
}

type ChatBuffer struct {
	mu  sync.Mutex
	m   map[int][]BufferedMsg
	ttl time.Duration
}

func NewChatBuffer(ttl time.Duration) *ChatBuffer {
	b := &ChatBuffer{
		m:   make(map[int][]BufferedMsg),
		ttl: ttl,
	}
	go b.cleaner()
	return b
}

func (b *ChatBuffer) Add(index int, text string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	arr := b.m[index]
	arr = append(arr, BufferedMsg{Text: text, At: time.Now()})
	if len(arr) > 10 {
		arr = arr[len(arr)-10:]
	}
	b.m[index] = arr
}

// PopMostRecent returns and removes the most recent message for the index
// that is within the provided window. Returns false if none found.
func (b *ChatBuffer) PopMostRecent(index int, window time.Duration) (string, bool) {
	if b == nil {
		return "", false
	}
	now := time.Now()
	b.mu.Lock()
	defer b.mu.Unlock()
	arr, ok := b.m[index]
	if !ok || len(arr) == 0 {
		return "", false
	}
	// search from the end for most recent valid
	for i := len(arr) - 1; i >= 0; i-- {
		if now.Sub(arr[i].At) <= window {
			msg := arr[i].Text
			// remove element i
			arr = append(arr[:i], arr[i+1:]...)
			if len(arr) == 0 {
				delete(b.m, index)
			} else {
				b.m[index] = arr
			}
			return msg, true
		}
	}
	return "", false
}

func (b *ChatBuffer) cleaner() {
	if b == nil {
		return
	}
	ticker := time.NewTicker(b.ttl / 2)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		b.mu.Lock()
		for idx, arr := range b.m {
			newArr := arr[:0]
			for _, m := range arr {
				if now.Sub(m.At) <= b.ttl {
					newArr = append(newArr, m)
				}
			}
			if len(newArr) == 0 {
				delete(b.m, idx)
			} else {
				b.m[idx] = newArr
			}
		}
		b.mu.Unlock()
	}
}
