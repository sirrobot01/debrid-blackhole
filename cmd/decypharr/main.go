package decypharr

import (
	"context"
	"fmt"
	"github.com/sirrobot01/decypharr/internal/config"
	"github.com/sirrobot01/decypharr/internal/logger"
	"github.com/sirrobot01/decypharr/pkg/qbit"
	"github.com/sirrobot01/decypharr/pkg/server"
	"github.com/sirrobot01/decypharr/pkg/service"
	"github.com/sirrobot01/decypharr/pkg/version"
	"github.com/sirrobot01/decypharr/pkg/web"
	"github.com/sirrobot01/decypharr/pkg/webdav"
	"github.com/sirrobot01/decypharr/pkg/worker"
	"os"
	"runtime/debug"
	"strconv"
	"sync"
)

func Start(ctx context.Context) error {

	if umaskStr := os.Getenv("UMASK"); umaskStr != "" {
		umask, err := strconv.ParseInt(umaskStr, 8, 32)
		if err != nil {
			return fmt.Errorf("invalid UMASK value: %s", umaskStr)
		}
		SetUmask(int(umask))
	}

	cfg := config.Get()
	var wg sync.WaitGroup
	errChan := make(chan error)

	_log := logger.GetDefaultLogger()

	_log.Info().Msgf("Starting Decypher (%s)", version.GetInfo().String())
	_log.Info().Msgf("Default Log Level: %s", cfg.LogLevel)

	svc := service.New()
	_qbit := qbit.New()
	srv := server.New()
	_webdav := webdav.New()

	ui := web.New(_qbit).Routes()
	webdavRoutes := _webdav.Routes()
	qbitRoutes := _qbit.Routes()

	// Register routes
	srv.Mount("/", ui)
	srv.Mount("/api/v2", qbitRoutes)
	srv.Mount("/webdav", webdavRoutes)

	safeGo := func(f func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					stack := debug.Stack()
					_log.Error().
						Interface("panic", r).
						Str("stack", string(stack)).
						Msg("Recovered from panic in goroutine")

					// Send error to channel so the main goroutine is aware
					errChan <- fmt.Errorf("panic: %v", r)
				}
			}()

			if err := f(); err != nil {
				errChan <- err
			}
		}()
	}

	safeGo(func() error {
		return _webdav.Start(ctx)
	})

	safeGo(func() error {
		return srv.Start(ctx)
	})

	safeGo(func() error {
		return worker.Start(ctx)
	})

	if cfg.Repair.Enabled {
		safeGo(func() error {
			err := svc.Repair.Start(ctx)
			if err != nil {
				_log.Error().Err(err).Msg("Error during repair")
			}
			return nil // Not propagating repair errors to terminate the app
		})
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
