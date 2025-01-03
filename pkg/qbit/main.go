package qbit

import (
	"context"
	"fmt"
	"goBlack/common"
	"goBlack/pkg/debrid"
	"goBlack/pkg/qbit/server"
)

func Start(ctx context.Context, config *common.Config, deb *debrid.DebridService) error {
	srv := server.NewServer(config, deb)
	if err := srv.Start(ctx); err != nil {
		return fmt.Errorf("failed to start qbit server: %w", err)
	}
	return nil
}
