package qbit

import (
	"context"
	"fmt"
	"github.com/sirrobot01/debrid-blackhole/pkg/arr"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid"
	"github.com/sirrobot01/debrid-blackhole/pkg/qbit/server"
)

func Start(ctx context.Context, deb *debrid.DebridService, arrs *arr.Storage) error {
	srv := server.NewServer(deb, arrs)
	if err := srv.Start(ctx); err != nil {
		return fmt.Errorf("failed to start qbit server: %w", err)
	}
	return nil
}
