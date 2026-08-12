package manager

import (
	"sync"
	"testing"
	"time"
)

// A reinsertAttempt can be mutated from more than one goroutine at once:
// reinsertDeletedTorrents runs once per provider, fanned out concurrently by
// syncTorrents, and reinsertAttempts is shared across providers keyed only by
// infohash — so the same torrent configured on two debrids hits the same
// *reinsertAttempt concurrently. snapshot/recordAttempt must serialize that:
// without the lock, `go test -race` flags it, and the plain count++ race
// also loses increments even without -race.
func TestReinsertAttempt_ConcurrentAccess(t *testing.T) {
	attempt := &reinsertAttempt{}

	const goroutines = 50
	const iterations = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				attempt.recordAttempt()
				attempt.snapshot()
			}
		}()
	}
	wg.Wait()

	count, lastTried := attempt.snapshot()
	if want := goroutines * iterations; count != want {
		t.Errorf("count = %d, want %d — concurrent recordAttempt calls lost updates", count, want)
	}
	if lastTried.IsZero() {
		t.Error("lastTried was never set")
	}
	if time.Since(lastTried) < 0 {
		t.Error("lastTried is in the future")
	}
}
