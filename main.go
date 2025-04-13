package main

import (
	"context"
	"flag"
	"github.com/sirrobot01/decypharr/cmd/decypharr"
	"github.com/sirrobot01/decypharr/internal/config"
	"github.com/sirrobot01/decypharr/pkg/version"
	"log"
	"net/http"
	_ "net/http/pprof" // registers pprof handlers
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
)

func main() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("FATAL: Recovered from panic in main: %v\n", r)
			debug.PrintStack()
		}
	}()

	if version.GetInfo().Channel == "dev" {
		log.Println("Running in dev mode")
		go func() {
			if err := http.ListenAndServe(":6060", nil); err != nil {
				log.Fatalf("pprof server failed: %v", err)
			}
		}()
	}
	var configPath string
	flag.StringVar(&configPath, "config", "/data", "path to the data folder")
	flag.Parse()

	if err := config.SetConfigPath(configPath); err != nil {
		log.Fatal(err)
	}

	config.Get()

	// Create a context that's cancelled on SIGINT/SIGTERM
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := decypharr.Start(ctx); err != nil {
		log.Fatal(err)
	}
}
