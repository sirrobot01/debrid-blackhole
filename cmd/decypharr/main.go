package decypharr

import (
	"context"
	"fmt"
	"github.com/rs/zerolog"
	"github.com/sirrobot01/debrid-blackhole/internal/config"
	"github.com/sirrobot01/debrid-blackhole/internal/logger"
	"github.com/sirrobot01/debrid-blackhole/pkg/proxy"
	"github.com/sirrobot01/debrid-blackhole/pkg/qbit"
	"github.com/sirrobot01/debrid-blackhole/pkg/server"
	"github.com/sirrobot01/debrid-blackhole/pkg/service"
	"github.com/sirrobot01/debrid-blackhole/pkg/version"
	"github.com/sirrobot01/debrid-blackhole/pkg/web"
	"github.com/sirrobot01/debrid-blackhole/pkg/webdav"
	"github.com/sirrobot01/debrid-blackhole/pkg/worker"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"sync"
	"syscall"
	"time"
)

func monitorGoroutines(interval time.Duration, _log zerolog.Logger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			_log.Debug().Msgf("Current goroutines: %d", runtime.NumGoroutine())
		}
	}
}

func Start(ctx context.Context) error {

	if umaskStr := os.Getenv("UMASK"); umaskStr != "" {
		umask, err := strconv.ParseInt(umaskStr, 8, 32)
		if err != nil {
			return fmt.Errorf("invalid UMASK value: %s", umaskStr)
		}
		// Set umask
		syscall.Umask(int(umask))
	}

	cfg := config.GetConfig()
	var wg sync.WaitGroup
	errChan := make(chan error)

	_log := logger.GetDefaultLogger()

	_log.Info().Msgf("Version: %s", version.GetInfo().String())
	_log.Debug().Msgf("Config Loaded: %s", cfg.JsonFile())
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

	if cfg.Proxy.Enabled {
		safeGo(func() error {
			return proxy.NewProxy().Start(ctx)
		})
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

	safeGo(func() error {
		monitorGoroutines(1*time.Minute, _log)
		return nil
	})

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
