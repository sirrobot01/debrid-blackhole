package worker

import (
	"context"
	"github.com/rs/zerolog"
	"github.com/sirrobot01/debrid-blackhole/internal/config"
	"github.com/sirrobot01/debrid-blackhole/internal/logger"
	"github.com/sirrobot01/debrid-blackhole/pkg/service"
	"os"
	"sync"
	"time"
)

var (
	_logInstance zerolog.Logger
	once         sync.Once
)

func getLogger() zerolog.Logger {

	once.Do(func() {
		cfg := config.GetConfig()
		_logInstance = logger.NewLogger("worker", cfg.LogLevel, os.Stdout)
	})
	return _logInstance
}

func Start(ctx context.Context) error {
	cfg := config.GetConfig()
	// Start Arr Refresh Worker

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		arrRefreshWorker(ctx, cfg)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		cleanUpQueuesWorker(ctx, cfg)
	}()
	wg.Wait()
	return nil
}

func arrRefreshWorker(ctx context.Context, cfg *config.Config) {
	// Start Arr Refresh Worker
	_logger := getLogger()
	_logger.Debug().Msg("Refresh Worker started")
	refreshCtx := context.WithValue(ctx, "worker", "refresh")
	refreshTicker := time.NewTicker(time.Duration(cfg.QBitTorrent.RefreshInterval) * time.Second)

	for {
		select {
		case <-refreshCtx.Done():
			_logger.Debug().Msg("Refresh Worker stopped")
			return
		case <-refreshTicker.C:
			refreshArrs()
		}
	}
}

func cleanUpQueuesWorker(ctx context.Context, cfg *config.Config) {
	// Start Clean up Queues Worker
	_logger := getLogger()
	_logger.Debug().Msg("Clean up Queues Worker started")
	cleanupCtx := context.WithValue(ctx, "worker", "cleanup")
	cleanupTicker := time.NewTicker(time.Duration(10) * time.Second)

	var cleanupMutex sync.Mutex

	for {
		select {
		case <-cleanupCtx.Done():
			_logger.Debug().Msg("Clean up Queues Worker stopped")
			return
		case <-cleanupTicker.C:
			if cleanupMutex.TryLock() {
				go func() {
					defer cleanupMutex.Unlock()
					cleanUpQueues()
				}()
			}
		}
	}
}

func refreshArrs() {
	for _, a := range service.GetService().Arr.GetAll() {
		err := a.Refresh()
		if err != nil {
			_logger := getLogger()
			_logger.Debug().Err(err).Msg("Error refreshing arr")
			return
		}
	}
}

func cleanUpQueues() {
	// Clean up queues
	_logger := getLogger()
	for _, a := range service.GetService().Arr.GetAll() {
		if !a.Cleanup {
			continue
		}
		_logger.Debug().Msgf("Cleaning up queue for %s", a.Name)
		if err := a.CleanupQueue(); err != nil {
			_logger.Debug().Err(err).Msg("Error cleaning up queue")
		}
	}
}
