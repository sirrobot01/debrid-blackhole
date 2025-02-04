package server

import (
	"context"
	"errors"
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"
	"github.com/sirrobot01/debrid-blackhole/internal/config"
	"github.com/sirrobot01/debrid-blackhole/internal/logger"
	"github.com/sirrobot01/debrid-blackhole/pkg/arr"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid"
	"github.com/sirrobot01/debrid-blackhole/pkg/qbit/shared"
	"github.com/sirrobot01/debrid-blackhole/pkg/repair"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

type Server struct {
	qbit   *shared.QBit
	logger zerolog.Logger
}

func NewServer(deb *debrid.DebridService, arrs *arr.Storage, _repair *repair.Repair) *Server {
	cfg := config.GetConfig()
	l := logger.NewLogger("QBit", cfg.QBitTorrent.LogLevel, os.Stdout)
	q := shared.NewQBit(deb, l, arrs, _repair)
	return &Server{
		qbit:   q,
		logger: l,
	}
}

func (s *Server) Start(ctx context.Context) error {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	logLevel := s.logger.GetLevel().String()
	debug := logLevel == "debug"
	q := QbitHandler{qbit: s.qbit, logger: s.logger, debug: debug}
	ui := UIHandler{
		qbit:   s.qbit,
		logger: logger.NewLogger("UI", s.logger.GetLevel().String(), os.Stdout),
		debug:  debug,
	}

	// Register routes
	r.Get("/logs", s.GetLogs)
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

func (s *Server) GetLogs(w http.ResponseWriter, r *http.Request) {
	logFile := logger.GetLogPath()

	// Open and read the file
	file, err := os.Open(logFile)
	if err != nil {
		http.Error(w, "Error reading log file", http.StatusInternalServerError)
		return
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			s.logger.Debug().Err(err).Msg("Error closing log file")
		}
	}(file)

	// Set headers
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", "inline; filename=application.log")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	// Stream the file
	_, err = io.Copy(w, file)
	if err != nil {
		s.logger.Debug().Err(err).Msg("Error streaming log file")
		http.Error(w, "Error streaming log file", http.StatusInternalServerError)
		return
	}
}
