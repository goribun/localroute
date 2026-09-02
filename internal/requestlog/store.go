package requestlog

import (
	"slices"
	"sync"
	"time"
)

type Event struct {
	ID         uint64    `json:"id"`
	Time       time.Time `json:"time"`
	Method     string    `json:"method"`
	Host       string    `json:"host"`
	Path       string    `json:"path"`
	Target     string    `json:"target"`
	RouteID    string    `json:"routeId"`
	RuleID     string    `json:"ruleId,omitempty"`
	Status     int       `json:"status"`
	DurationMS int64     `json:"durationMs"`
	Error      string    `json:"error,omitempty"`
}
type Store struct {
	mu          sync.RWMutex
	events      []Event
	limit       int
	nextID      uint64
	subscribers map[chan Event]struct{}
}

func New(limit int) *Store {
	if limit < 1 {
		limit = 1000
	}
	return &Store{limit: limit, subscribers: map[chan Event]struct{}{}}
}
func (s *Store) Add(event Event) {
	s.mu.Lock()
	s.nextID++
	event.ID = s.nextID
	s.events = append(s.events, event)
	if n := len(s.events) - s.limit; n > 0 {
		s.events = slices.Clone(s.events[n:])
	}
	for ch := range s.subscribers {
		select {
		case ch <- event:
		default:
		}
	}
	s.mu.Unlock()
}
func (s *Store) List() []Event { s.mu.RLock(); defer s.mu.RUnlock(); return slices.Clone(s.events) }
func (s *Store) Clear()        { s.mu.Lock(); s.events = nil; s.mu.Unlock() }
func (s *Store) Subscribe() (chan Event, func()) {
	ch := make(chan Event, 64)
	s.mu.Lock()
	s.subscribers[ch] = struct{}{}
	s.mu.Unlock()
	return ch, func() { s.mu.Lock(); delete(s.subscribers, ch); close(ch); s.mu.Unlock() }
}
