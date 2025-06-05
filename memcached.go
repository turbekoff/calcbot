package main

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrMemcachedClosed = errors.New("memcached closed")

type cached struct {
	value    interface{}
	expireAt int64
}

type Memcached struct {
	mu             sync.RWMutex
	stopOnce       sync.Once
	stopCh         chan struct{}
	doneCh         chan struct{}
	items          map[string]cached
	ttlTimeout     time.Duration
	isShuttingDown bool
}

func NewMemcached(ttlTimeout, cleanupTimeout time.Duration) *Memcached {
	mc := &Memcached{
		doneCh:     make(chan struct{}),
		stopCh:     make(chan struct{}),
		items:      make(map[string]cached),
		ttlTimeout: ttlTimeout,
	}

	go func() {
		ticker := time.NewTicker(cleanupTimeout)
		defer ticker.Stop()
		defer close(mc.doneCh)

		for {
			select {
			case <-mc.stopCh:
				return
			case <-ticker.C:
				now := time.Now().UnixNano()
				mc.mu.Lock()
				for k, v := range mc.items {
					if now > v.expireAt {
						delete(mc.items, k)
					}
				}

				isDone := mc.isShuttingDown && len(mc.items) == 0
				mc.mu.Unlock()

				if isDone {
					return
				}
			}
		}
	}()
	return mc
}

func (mc *Memcached) Set(key string, value interface{}) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	_, isExists := mc.items[key]
	if mc.isShuttingDown && !isExists {
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

func (mc *Memcached) Shutdown(ctx context.Context) error {
	result := ErrMemcachedClosed

	mc.stopOnce.Do(func() {
		mc.mu.Lock()
		mc.isShuttingDown = true
		mc.mu.Unlock()

		select {
		case <-mc.doneCh:
			result = ErrMemcachedClosed
		case <-ctx.Done():
			close(mc.stopCh)
			result = ctx.Err()
		}
	})

	return result
}
