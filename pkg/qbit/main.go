package qbit

import (
	"context"
	"fmt"
	"github.com/sirrobot01/debrid-blackhole/pkg/arr"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid"
	"github.com/sirrobot01/debrid-blackhole/pkg/qbit/server"
	"github.com/sirrobot01/debrid-blackhole/pkg/repair"
)

func Start(ctx context.Context, deb *debrid.DebridService, arrs *arr.Storage, _repair *repair.Repair) error {
	srv := server.NewServer(deb, arrs, _repair)
	if err := srv.Start(ctx); err != nil {
		return fmt.Errorf("failed to start qbit server: %w", err)
	}
	return nil
}
