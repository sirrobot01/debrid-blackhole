package cmd

import (
	"context"
	"github.com/sirrobot01/debrid-blackhole/internal/config"
	"github.com/sirrobot01/debrid-blackhole/pkg/arr"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid"
	"github.com/sirrobot01/debrid-blackhole/pkg/proxy"
	"github.com/sirrobot01/debrid-blackhole/pkg/qbit"
	"github.com/sirrobot01/debrid-blackhole/pkg/repair"
	"log"
	"sync"
)

func Start(ctx context.Context) error {
	cfg := config.GetConfig()

	deb := debrid.NewDebrid()
	arrs := arr.NewStorage()
	_repair := repair.NewRepair(deb.Get(), arrs)

	var wg sync.WaitGroup
	errChan := make(chan error, 2)

	if cfg.Proxy.Enabled {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := proxy.NewProxy(deb).Start(ctx); err != nil {
				errChan <- err
			}
		}()
	}
	if cfg.QBitTorrent.Port != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := qbit.Start(ctx, deb, arrs, _repair); err != nil {
				errChan <- err
			}
		}()
	}

	if cfg.Repair.Enabled {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := _repair.Start(ctx); err != nil {
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
