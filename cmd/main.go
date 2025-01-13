package cmd

import (
	"cmp"
	"context"
	"github.com/sirrobot01/debrid-blackhole/common"
	"github.com/sirrobot01/debrid-blackhole/pkg/arr"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid"
	"github.com/sirrobot01/debrid-blackhole/pkg/proxy"
	"github.com/sirrobot01/debrid-blackhole/pkg/qbit"
	"github.com/sirrobot01/debrid-blackhole/pkg/repair"
	"log"
	"sync"
)

func Start(ctx context.Context, config *common.Config) error {
	maxCacheSize := cmp.Or(config.MaxCacheSize, 1000)

	deb := debrid.NewDebrid(config.Debrids, maxCacheSize)
	arrs := arr.NewStorage(config.Arrs)

	var wg sync.WaitGroup
	errChan := make(chan error, 2)

	if config.Proxy.Enabled {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := proxy.NewProxy(*config, deb).Start(ctx); err != nil {
				errChan <- err
			}
		}()
	}
	if config.QBitTorrent.Port != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := qbit.Start(ctx, config, deb, arrs); err != nil {
				errChan <- err
			}
		}()
	}

	if config.Repair.Enabled {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := repair.Start(ctx, config, arrs); err != nil {
				log.Printf("Error during repair: %v", err)
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
