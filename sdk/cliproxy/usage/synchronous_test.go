package usage

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

type synchronousTestSink struct{ calls atomic.Int64 }

func (s *synchronousTestSink) SynchronousUsage() bool              { return true }
func (s *synchronousTestSink) HandleUsage(context.Context, Record) { s.calls.Add(1) }

type asyncTestSink struct{ received chan Record }

func (s *asyncTestSink) HandleUsage(_ context.Context, r Record) { s.received <- r }

func TestSynchronousSinkSettlesBeforePublishReturnsAndIsNotQueued(t *testing.T) {
	m := NewManager(1)
	defer m.Stop()
	s := &synchronousTestSink{}
	a := &asyncTestSink{received: make(chan Record, 1)}
	m.Register(s)
	m.Register(a)
	m.Publish(context.Background(), Record{RequestID: "durable"})
	if s.calls.Load() != 1 {
		t.Fatal("durable sink did not settle before publication returned")
	}
	select {
	case r := <-a.received:
		if r.RequestID != "durable" {
			t.Fatal("async record changed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("async sink not delivered")
	}
	if s.calls.Load() != 1 {
		t.Fatal("durable sink was delivered again by async queue")
	}
	m.Stop()
	m.Publish(context.Background(), Record{RequestID: "closed"})
	if s.calls.Load() != 1 {
		t.Fatal("closed dispatcher still published")
	}
}
