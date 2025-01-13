package server

import (
	"context"
	"errors"
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/sirrobot01/debrid-blackhole/common"
	"github.com/sirrobot01/debrid-blackhole/pkg/arr"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid"
	"github.com/sirrobot01/debrid-blackhole/pkg/qbit/shared"
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

func NewServer(config *common.Config, deb *debrid.DebridService, arrs *arr.Storage) *Server {
	logger := common.NewLogger("QBit", os.Stdout)
	q := shared.NewQBit(config, deb, logger, arrs)
	return &Server{
		qbit:   q,
		logger: logger,
		debug:  config.QBitTorrent.Debug,
	}
}

func (s *Server) Start(ctx context.Context) error {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	q := qbitHandler{qbit: s.qbit, logger: s.logger}
	ui := uiHandler{qbit: s.qbit, logger: common.NewLogger("UI", os.Stdout)}

	// Register routes
	q.Routes(r)
	ui.Routes(r)

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
