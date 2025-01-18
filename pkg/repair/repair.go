package repair

import (
	"context"
	"github.com/sirrobot01/debrid-blackhole/common"
	"github.com/sirrobot01/debrid-blackhole/pkg/arr"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func Start(ctx context.Context, config *common.Config, arrs *arr.Storage) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	logger := common.NewLogger("Repair", config.LogLevel, os.Stdout)
	defer stop()

	duration, err := parseSchedule(config.Repair.Interval)
	if err != nil {
		log.Fatalf("Failed to parse schedule: %v", err)
	}

	if config.Repair.RunOnStart {
		logger.Info().Msgf("Running initial repair")
		if err := repair(arrs); err != nil {
			log.Printf("Error during initial repair: %v", err)
			return err
		}
	}

	ticker := time.NewTicker(duration)
	defer ticker.Stop()

	if strings.Contains(config.Repair.Interval, ":") {
		logger.Info().Msgf("Starting repair worker, scheduled daily at %s", config.Repair.Interval)
	} else {
		logger.Info().Msgf("Starting repair worker with %v interval", duration)
	}

	for {
		select {
		case <-ctx.Done():
			logger.Info().Msg("Repair worker stopped")
			return nil
		case t := <-ticker.C:
			logger.Info().Msgf("Running repair at %v", t.Format("15:04:05"))
			if err := repair(arrs); err != nil {
				logger.Info().Msgf("Error during repair: %v", err)
				return err
			}

			// If using time-of-day schedule, reset the ticker for next day
			if strings.Contains(config.Repair.Interval, ":") {
				nextDuration, err := parseSchedule(config.Repair.Interval)
				if err != nil {
					logger.Info().Msgf("Error calculating next schedule: %v", err)
					return err
				}
				ticker.Reset(nextDuration)
			}
		}
	}
}

func repair(arrs *arr.Storage) error {
	for _, a := range arrs.GetAll() {
		go a.Repair("")
	}
	return nil
}
