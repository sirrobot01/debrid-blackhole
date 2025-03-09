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
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

type Server struct {
	router *chi.Mux
	logger zerolog.Logger
}

func New() *Server {
	cfg := config.GetConfig()
	l := logger.NewLogger("http", cfg.LogLevel, os.Stdout)
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	return &Server{
		router: r,
		logger: l,
	}
}

func (s *Server) Start(ctx context.Context) error {
	cfg := config.GetConfig()
	// Register routes
	// Register webhooks
	s.router.Post("/webhooks/tautulli", s.handleTautulli)

	// Register logs
	s.router.Get("/logs", s.getLogs)
	port := fmt.Sprintf(":%s", cfg.QBitTorrent.Port)
	s.logger.Info().Msgf("Starting server on %s", port)
	srv := &http.Server{
		Addr:    port,
		Handler: s.router,
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

func (s *Server) AddRoutes(routes func(r chi.Router) http.Handler) {
	routes(s.router)
}

func (s *Server) Mount(pattern string, handler http.Handler) {
	s.router.Mount(pattern, handler)
}

func (s *Server) getLogs(w http.ResponseWriter, r *http.Request) {
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
