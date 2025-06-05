package main

import (
	"context"
	"errors"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

var ErrMemcachedClosed = errors.New("memcached closed")

type cached struct {
	value    interface{}
	expireAt int64
}

type Memcached struct {
	mu          sync.RWMutex
	cleanerOnce sync.Once
	cleanerCh   chan struct{}
	items       map[string]cached
	ttlTimeout  time.Duration
	inShutdown  atomic.Bool
}

func NewMemcached(ttlTimeout, cleanupTimeout time.Duration) *Memcached {
	mc := &Memcached{
		cleanerCh:  make(chan struct{}),
		items:      make(map[string]cached),
		ttlTimeout: ttlTimeout,
	}

	go func() {
		ticker := time.NewTicker(cleanupTimeout)
		defer ticker.Stop()

		for {
			select {
			case <-mc.cleanerCh:
				return
			case <-ticker.C:
				mc.cleanExpiredItems()
			}
		}
	}()
	return mc
}

func (mc *Memcached) Set(key string, value interface{}) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	_, isExists := mc.items[key]
	if mc.inShutdown.Load() && !isExists {
		return
	}

	expireAt := time.Now().Add(mc.ttlTimeout).UnixNano()
	mc.items[key] = cached{
		value:    value,
		expireAt: expireAt,
	}
}

func (mc *Memcached) Get(key string) interface{} {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	item, exists := mc.items[key]
	if !exists {
		return nil
	}

	if time.Now().UnixNano() > item.expireAt {
		return nil
	}
	return item.value
}

const shutdownIntervalMax = 500 * time.Millisecond

func (mc *Memcached) Shutdown(ctx context.Context) error {
	mc.mu.Lock()
	mc.inShutdown.Store(true)
	mc.mu.Unlock()
	mc.closeCleaner()

	intervalBase := time.Millisecond
	nextInterval := func() time.Duration {
		interval := intervalBase + time.Duration(rand.Intn(int(intervalBase/10)))

		intervalBase *= 2
		if intervalBase > shutdownIntervalMax {
			intervalBase = shutdownIntervalMax
		}
		return interval
	}

	timer := time.NewTimer(nextInterval())
	defer timer.Stop()
	for {
		mc.cleanExpiredItems()
		if mc.IsEmpty() {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			timer.Reset(nextInterval())
		}
	}
}

func (mc *Memcached) Close() error {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	if mc.inShutdown.Load() {
		return ErrMemcachedClosed
	}

	mc.inShutdown.Store(true)
	mc.closeCleaner()

	for k := range mc.items {
		delete(mc.items, k)
	}
	return nil
}

func (mc *Memcached) cleanExpiredItems() {
	now := time.Now().UnixNano()
	mc.mu.Lock()
	defer mc.mu.Unlock()

	for k, v := range mc.items {
		if now > v.expireAt {
			delete(mc.items, k)
		}
	}
}

func (mc *Memcached) IsEmpty() bool {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	return len(mc.items) == 0
}

func (mc *Memcached) closeCleaner() {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.cleanerOnce.Do(func() {
		close(mc.cleanerCh)
	})
}
