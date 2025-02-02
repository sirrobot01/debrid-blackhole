package repair

import (
	"context"
	"github.com/sirrobot01/debrid-blackhole/internal/config"
	"github.com/sirrobot01/debrid-blackhole/internal/logger"
	"github.com/sirrobot01/debrid-blackhole/pkg/arr"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func Start(ctx context.Context, arrs *arr.Storage) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	cfg := config.GetConfig()
	repairConfig := cfg.Repair
	_logger := logger.NewLogger("Repair", cfg.LogLevel, os.Stdout)
	defer stop()

	duration, err := parseSchedule(repairConfig.Interval)
	if err != nil {
		log.Fatalf("Failed to parse schedule: %v", err)
	}

	if repairConfig.RunOnStart {
		_logger.Info().Msgf("Running initial repair")
		if err := repair(arrs); err != nil {
			log.Printf("Error during initial repair: %v", err)
			return err
		}
	}

	ticker := time.NewTicker(duration)
	defer ticker.Stop()

	if strings.Contains(repairConfig.Interval, ":") {
		_logger.Info().Msgf("Starting repair worker, scheduled daily at %s", repairConfig.Interval)
	} else {
		_logger.Info().Msgf("Starting repair worker with %v interval", duration)
	}

	for {
		select {
		case <-ctx.Done():
			_logger.Info().Msg("Repair worker stopped")
			return nil
		case t := <-ticker.C:
			_logger.Info().Msgf("Running repair at %v", t.Format("15:04:05"))
			if err := repair(arrs); err != nil {
				_logger.Info().Msgf("Error during repair: %v", err)
				return err
			}

			// If using time-of-day schedule, reset the ticker for next day
			if strings.Contains(repairConfig.Interval, ":") {
				nextDuration, err := parseSchedule(repairConfig.Interval)
				if err != nil {
					_logger.Info().Msgf("Error calculating next schedule: %v", err)
					return err
				}
				ticker.Reset(nextDuration)
			}
		}
	}
}

func repair(arrs *arr.Storage) error {
	for _, a := range arrs.GetAll() {
		go func() {
			err := a.Repair("")
			if err != nil {
				log.Printf("Error repairing %s: %v", a.Name, err)
			}
		}()
	}
	return nil
}
