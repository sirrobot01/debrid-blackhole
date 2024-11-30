package cmd

import (
	"cmp"
	"context"
	"goBlack/common"
	"goBlack/pkg/debrid"
	"goBlack/pkg/proxy"
	"goBlack/pkg/qbit"
	"sync"
)

func Start(ctx context.Context, config *common.Config) error {
	maxCacheSize := cmp.Or(config.MaxCacheSize, 1000)
	cache := common.NewCache(maxCacheSize)

	deb := debrid.NewDebrid(config.Debrids, cache)

	var wg sync.WaitGroup
	errChan := make(chan error, 2)

	if config.Proxy.Enabled {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := proxy.NewProxy(*config, deb, cache).Start(ctx); err != nil {
				errChan <- err
			}
		}()
	}
	if config.QBitTorrent.Port != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := qbit.Start(ctx, config, deb, cache); err != nil {
				errChan <- err
			}
		}()
	}

	go func() {
		wg.Wait()
		close(errChan)
	}()

	// Wait for context cancellation or completion or error
	select {
	case err := <-errChan:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
