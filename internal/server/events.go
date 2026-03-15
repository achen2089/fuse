package server

import (
	"sync"

	"fuse/internal/domain"
)

type Broadcaster struct {
	mu   sync.Mutex
	subs map[chan domain.Event]struct{}
}

func NewBroadcaster() *Broadcaster {
	return &Broadcaster{
		subs: map[chan domain.Event]struct{}{},
	}
}

func (b *Broadcaster) Subscribe() chan domain.Event {
	ch := make(chan domain.Event, 16)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *Broadcaster) Unsubscribe(ch chan domain.Event) {
	b.mu.Lock()
	if _, ok := b.subs[ch]; ok {
		delete(b.subs, ch)
		close(ch)
	}
	b.mu.Unlock()
}

func (b *Broadcaster) Publish(event domain.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- event:
		default:
		}
	}
}
