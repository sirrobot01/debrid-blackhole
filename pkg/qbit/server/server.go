package server

import (
	"context"
	"errors"
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"
	"github.com/sirrobot01/debrid-blackhole/common"
	"github.com/sirrobot01/debrid-blackhole/pkg/arr"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid"
	"github.com/sirrobot01/debrid-blackhole/pkg/qbit/shared"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

type Server struct {
	qbit   *shared.QBit
	logger zerolog.Logger
}

func NewServer(config *common.Config, deb *debrid.DebridService, arrs *arr.Storage) *Server {
	logger := common.NewLogger("QBit", config.QBitTorrent.LogLevel, os.Stdout)
	q := shared.NewQBit(config, deb, logger, arrs)
	return &Server{
		qbit:   q,
		logger: logger,
	}
}

func (s *Server) Start(ctx context.Context) error {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	logLevel := s.logger.GetLevel().String()
	debug := logLevel == "debug"
	q := qbitHandler{qbit: s.qbit, logger: s.logger, debug: debug}
	ui := uiHandler{qbit: s.qbit, logger: common.NewLogger("UI", s.logger.GetLevel().String(), os.Stdout), debug: debug}

	// Register routes
	q.Routes(r)
	ui.Routes(r)

	go s.qbit.StartWorker(context.Background())

	s.logger.Info().Msgf("Starting QBit server on :%s", s.qbit.Port)
	port := fmt.Sprintf(":%s", s.qbit.Port)
	srv := &http.Server{
		Addr:    port,
		Handler: r,
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Info().Msgf("Error starting server: %v", err)
			stop()
		}
	}()

	<-ctx.Done()
	s.logger.Info().Msg("Shutting down gracefully...")
	return srv.Shutdown(context.Background())
}
