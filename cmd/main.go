package cmd

import (
	"context"
	"github.com/sirrobot01/debrid-blackhole/internal/config"
	"github.com/sirrobot01/debrid-blackhole/internal/logger"
	"github.com/sirrobot01/debrid-blackhole/pkg/proxy"
	"github.com/sirrobot01/debrid-blackhole/pkg/qbit"
	"github.com/sirrobot01/debrid-blackhole/pkg/server"
	"github.com/sirrobot01/debrid-blackhole/pkg/service"
	"github.com/sirrobot01/debrid-blackhole/pkg/version"
	"github.com/sirrobot01/debrid-blackhole/pkg/web"
	"log"
	"sync"
)

func Start(ctx context.Context) error {
	cfg := config.GetConfig()
	var wg sync.WaitGroup
	errChan := make(chan error)

	_log := logger.GetLogger(cfg.LogLevel)

	_log.Info().Msgf("Version: %s", version.GetInfo().String())
	_log.Debug().Msgf("Config Loaded: %s", cfg.JsonFile())
	_log.Debug().Msgf("Default Log Level: %s", cfg.LogLevel)

	svc := service.New()
	_qbit := qbit.New()
	_proxy := proxy.NewProxy()
	srv := server.New()
	webRoutes := web.New(_qbit).Routes()
	qbitRoutes := _qbit.Routes()

	// Register routes
	srv.Mount("/", webRoutes)
	srv.Mount("/api/v2", qbitRoutes)

	if cfg.Proxy.Enabled {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := _proxy.Start(ctx); err != nil {
				errChan <- err
			}
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := srv.Start(ctx); err != nil {
			errChan <- err
		}

	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		_qbit.StartWorker(ctx)
	}()

	if cfg.Repair.Enabled {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := svc.Repair.Start(ctx); err != nil {
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
