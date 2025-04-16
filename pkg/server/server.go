package server

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/goccy/go-json"
	"github.com/rs/zerolog"
	"github.com/sirrobot01/decypharr/internal/config"
	"github.com/sirrobot01/decypharr/internal/logger"
	"io"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
)

type Server struct {
	router *chi.Mux
	logger zerolog.Logger
}

func New() *Server {
	l := logger.New("http")
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	return &Server{
		router: r,
		logger: l,
	}
}

func (s *Server) Start(ctx context.Context) error {
	cfg := config.Get()
	// Register routes
	// Register webhooks
	s.router.Post("/webhooks/tautulli", s.handleTautulli)

	// Register logs
	s.router.Get("/logs", s.getLogs)
	s.router.Get("/stats", s.getStats)
	p := cmp.Or(cfg.QBitTorrent.Port, "8282")
	port := fmt.Sprintf(":%s", p)
	s.logger.Info().Msgf("Server started on %s", port)
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
			s.logger.Error().Err(err).Msg("Error closing log file")
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
		s.logger.Error().Err(err).Msg("Error streaming log file")
		http.Error(w, "Error streaming log file", http.StatusInternalServerError)
		return
	}
}

func (s *Server) getStats(w http.ResponseWriter, r *http.Request) {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	stats := map[string]interface{}{
		// Memory stats
		"heap_alloc_mb":  fmt.Sprintf("%.2fMB", float64(memStats.HeapAlloc)/1024/1024),
		"total_alloc_mb": fmt.Sprintf("%.2fMB", float64(memStats.TotalAlloc)/1024/1024),
		"sys_mb":         fmt.Sprintf("%.2fMB", float64(memStats.Sys)/1024/1024),

		// GC stats
		"gc_cycles": memStats.NumGC,
		// Goroutine stats
		"goroutines": runtime.NumGoroutine(),

		// System info
		"num_cpu": runtime.NumCPU(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(stats); err != nil {
		s.logger.Error().Err(err).Msg("Failed to encode stats")
	}
}
