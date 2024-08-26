package cmd

import (
	"goBlack/common"
	"goBlack/debrid"
	"sync"
)

func Start(config *common.Config) {

	deb := debrid.NewDebrid(config.Debrid)

	var wg sync.WaitGroup

	if config.Proxy.Enabled {
		proxy := NewProxy(*config, deb)
		wg.Add(1)
		go func() {
			defer wg.Done()
			proxy.Start()
		}()
	}

	if len(config.Arrs) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			StartBlackhole(config, deb)
		}()
	}

	// Wait indefinitely
	wg.Wait()

}
