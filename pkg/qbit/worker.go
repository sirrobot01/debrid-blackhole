package qbit

import (
	"context"
	"github.com/sirrobot01/debrid-blackhole/pkg/service"
	"time"
)

func (q *QBit) StartWorker(ctx context.Context) {
	q.logger.Info().Msg("Qbit Worker started")
	q.StartRefreshWorker(ctx)
}

func (q *QBit) StartRefreshWorker(ctx context.Context) {
	refreshCtx := context.WithValue(ctx, "worker", "refresh")
	refreshTicker := time.NewTicker(time.Duration(q.RefreshInterval) * time.Second)
	for {
		select {
		case <-refreshCtx.Done():
			q.logger.Info().Msg("Qbit Refresh Worker stopped")
			return
		case <-refreshTicker.C:
			torrents := q.Storage.GetAll("", "", nil)
			if len(torrents) > 0 {
				q.RefreshArrs()
			}
		}
	}
}

func (q *QBit) RefreshArrs() {
	arrs := service.GetService().Arr
	for _, arr := range arrs.GetAll() {
		err := arr.Refresh()
		if err != nil {
			return
		}
	}
}
