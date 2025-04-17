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
	"net/http"
	"os"
	"runtime/debug"
	"strconv"
	"sync"
	"time"
)

func Start(ctx context.Context) error {

	if umaskStr := os.Getenv("UMASK"); umaskStr != "" {
		umask, err := strconv.ParseInt(umaskStr, 8, 32)
		if err != nil {
			return fmt.Errorf("invalid UMASK value: %s", umaskStr)
		}
		SetUmask(int(umask))
	}

	appCtx := ctx

	// Service context - can be cancelled and recreated for restarts
	svcCtx, cancelSvc := context.WithCancel(context.Background())

	// Create a channel to listen for restart signals
	restartCh := make(chan struct{}, 1)

	// Create a function to expose for requesting restarts
	RequestRestart := func() {
		select {
		case restartCh <- struct{}{}:
			// Signal sent successfully
		default:
			// Channel is full, ignore
		}
	}

	web.SetRestartFunc(RequestRestart)

	go func() {
		for {
			select {
			case <-appCtx.Done():
				// Parent context is done, exit the loop and shut down all services
				cancelSvc()
				return
			case <-restartCh:
				_log := logger.Default()
				_log.Info().Msg("Restarting services with new config...")

				// Cancel current service context to shut down all services
				cancelSvc()

				// Wait a moment for services to shut down
				time.Sleep(500 * time.Millisecond)

				// Create a new service context
				svcCtx, cancelSvc = context.WithCancel(context.Background())

				// Reload configuration
				config.Reload()

				// Start services again with new context
				go func() {
					err := startServices(svcCtx)
					if err != nil {
						_log.Error().Err(err).Msg("Error restarting services")
					}
				}()

				_log.Info().Msg("Services restarted successfully")
			}
		}
	}()

	go func() {
		err := startServices(svcCtx)
		if err != nil {
			_log := logger.Default()
			_log.Error().Err(err).Msg("Error starting services")
		}
	}()

	// Start services for the first time
	<-appCtx.Done()

	// Clean up
	cancelSvc()
	return nil
}

func startServices(ctx context.Context) error {
	cfg := config.Get()
	var wg sync.WaitGroup
	errChan := make(chan error)

	_log := logger.Default()

	asciiArt := `
+-------------------------------------------------------+
|                                                       |
|  ╔╦╗╔═╗╔═╗╦ ╦╔═╗╦ ╦╔═╗╦═╗╦═╗                          |
|   ║║║╣ ║  └┬┘╠═╝╠═╣╠═╣╠╦╝╠╦╝ (%s)		|
|  ═╩╝╚═╝╚═╝ ┴ ╩  ╩ ╩╩ ╩╩╚═╩╚═                          |
|                                                       |
+-------------------------------------------------------+
|  Log Level: %s                         		|
+-------------------------------------------------------+
`

	fmt.Printf(asciiArt, version.GetInfo(), cfg.LogLevel)

	svc := service.New()
	_qbit := qbit.New()
	_webdav := webdav.New()

	ui := web.New(_qbit).Routes()
	webdavRoutes := _webdav.Routes()
	qbitRoutes := _qbit.Routes()

	// Register routes
	handlers := map[string]http.Handler{
		"/":       ui,
		"/api/v2": qbitRoutes,
		"/webdav": webdavRoutes,
	}
	srv := server.New(handlers)

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

	go func() {
		for err := range errChan {
			if err != nil {
				_log.Error().Err(err).Msg("Service error detected")
				// Don't shut down the whole app
			}
		}
	}()

	// Wait for context cancellation
	<-ctx.Done()
	_log.Debug().Msg("Services context cancelled")
	return nil
}
