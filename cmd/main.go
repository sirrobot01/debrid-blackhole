package cmd

import (
	"cmp"
	"goBlack/common"
	"goBlack/debrid"
	"sync"
)

func Start(config *common.Config) {
	maxCacheSize := cmp.Or(config.MaxCacheSize, 1000)
	cache := common.NewCache(maxCacheSize)
	
	deb := debrid.NewDebrid(config.Debrid, cache)

	var wg sync.WaitGroup

	if config.Proxy.Enabled {
		proxy := NewProxy(*config, deb, cache)
		wg.Add(1)
		go func() {
			defer wg.Done()
			proxy.Start()
		}()
	}

	if len(config.Arrs) > 0 {
		blackhole := NewBlackhole(config, deb, cache)
		wg.Add(1)
		go func() {
			defer wg.Done()
			blackhole.Start()
		}()
	}

	// Wait indefinitely
	wg.Wait()

}
