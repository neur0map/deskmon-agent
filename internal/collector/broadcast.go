package collector

import "sync"

// Broadcaster is a generic fan-out pub/sub for collector events.
// Subscribers receive events on a buffered channel. Slow subscribers
// are dropped (non-blocking send) to avoid back-pressure.
type Broadcaster[T any] struct {
	mu   sync.Mutex
	subs map[uint64]chan T
	next uint64
}

// NewBroadcaster creates a ready-to-use broadcaster.
func NewBroadcaster[T any]() *Broadcaster[T] {
	return &Broadcaster[T]{
		subs: make(map[uint64]chan T),
	}
}

// Subscribe returns a channel that receives broadcast events and a
// cleanup function the caller must invoke when done.
func (b *Broadcaster[T]) Subscribe(bufSize int) (<-chan T, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()

	id := b.next
	b.next++
	ch := make(chan T, bufSize)
	b.subs[id] = ch

	return ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		delete(b.subs, id)
		// Drain and close so readers unblock
		close(ch)
	}
}

// Send delivers val to every subscriber. Subscribers whose buffer is
// full are silently skipped.
func (b *Broadcaster[T]) Send(val T) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, ch := range b.subs {
		select {
		case ch <- val:
		default:
		}
	}
}
