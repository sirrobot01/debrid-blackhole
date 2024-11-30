package server

import (
	"context"
	"errors"
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"goBlack/common"
	"goBlack/pkg/debrid"
	"goBlack/pkg/qbit/shared"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

type Server struct {
	qbit   *shared.QBit
	logger *log.Logger
	debug  bool
}

func NewServer(config *common.Config, deb *debrid.DebridService, cache *common.Cache) *Server {
	logger := common.NewLogger("QBit", os.Stdout)
	q := shared.NewQBit(config, deb, cache, logger)
	return &Server{
		qbit:   q,
		logger: logger,
		debug:  config.QBitTorrent.Debug,
	}
}

func (s *Server) Start(ctx context.Context) error {
	r := chi.NewRouter()
	if s.debug {
		r.Use(middleware.Logger)
	}
	r.Use(middleware.Recoverer)
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	s.Routes(r)

	go s.qbit.StartWorker(context.Background())

	s.logger.Printf("Starting QBit server on :%s", s.qbit.Port)
	port := fmt.Sprintf(":%s", s.qbit.Port)
	srv := &http.Server{
		Addr:    port,
		Handler: r,
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Printf("Error starting server: %v\n", err)
			stop()
		}
	}()

	<-ctx.Done()
	fmt.Println("Shutting down gracefully...")
	return srv.Shutdown(context.Background())
}
