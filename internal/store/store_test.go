package store

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func TestAtomicPublication(t *testing.T) {
	s := Local{Dir: t.TempDir()}
	ctx := context.Background()
	k := "glyphs/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.json"
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e := s.PutIfAbsent(ctx, k, []byte("complete"), "application/json")
			if e == nil {
				wins.Add(1)
			} else if !errors.Is(e, ErrExists) {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatal(wins.Load())
	}
	b, e := s.Get(ctx, k)
	if e != nil || string(b) != "complete" {
		t.Fatalf("%s %v", b, e)
	}
	if _, e = s.Get(ctx, "../../etc/passwd"); e == nil {
		t.Fatal("traversal allowed")
	}
}
