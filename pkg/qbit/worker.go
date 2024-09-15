package qbit

import (
	"context"
	"goBlack/pkg/debrid"
	"time"
)

func (q *QBit) StartWorker(ctx context.Context) {
	q.logger.Println("Qbit Worker started")
	q.StartRefreshWorker(ctx)
}

func (q *QBit) StartRefreshWorker(ctx context.Context) {
	refreshCtx := context.WithValue(ctx, "worker", "refresh")
	refreshTicker := time.NewTicker(time.Duration(q.RefreshInterval) * time.Second)
	for {
		select {
		case <-refreshCtx.Done():
			q.logger.Println("Qbit Refresh Worker stopped")
			return
		case <-refreshTicker.C:
			q.RefreshArrs()
		}
	}
}

func (q *QBit) RefreshArrs() {
	torrents := q.storage.GetAll("", "", nil)
	if len(torrents) == 0 {
		return
	}
	for host, token := range q.arrs {
		arr := &debrid.Arr{
			Name:  "",
			Token: token,
			Host:  host,
		}
		q.RefreshArr(arr)
	}
}
