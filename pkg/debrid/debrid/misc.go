package debrid

import "sync"

func tryLock(mu *sync.Mutex, f func()) {
	if mu.TryLock() {
		defer mu.Unlock()
		f()
	}
}
