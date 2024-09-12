package cmd

import (
	"cmp"
	"goBlack/common"
	"goBlack/pkg/debrid"
	"goBlack/pkg/proxy"
	"goBlack/pkg/qbit"
	"sync"
)

func Start(config *common.Config) {
	maxCacheSize := cmp.Or(config.MaxCacheSize, 1000)
	cache := common.NewCache(maxCacheSize)

	deb := debrid.NewDebrid(config.Debrid, cache)

	var wg sync.WaitGroup

	if config.Proxy.Enabled {
		p := proxy.NewProxy(*config, deb, cache)
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.Start()
		}()
	}
	if config.QBitTorrent.Port != "" {
		qb := qbit.NewQBit(config, deb, cache)
		wg.Add(1)
		go func() {
			defer wg.Done()
			qb.Start()
		}()
	}

	// Wait indefinitely
	wg.Wait()

}
