package main

import (
	"context"
	"flag"
	"github.com/sirrobot01/debrid-blackhole/cmd/decypharr"
	"github.com/sirrobot01/debrid-blackhole/internal/config"
	"log"
	"runtime/debug"
)

func main() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("FATAL: Recovered from panic in main: %v\n", r)
			debug.PrintStack()
		}
	}()
	var configPath string
	flag.StringVar(&configPath, "config", "/data", "path to the data folder")
	flag.Parse()

	if err := config.SetConfigPath(configPath); err != nil {
		log.Fatal(err)
	}
	config.GetConfig()
	ctx := context.Background()
	if err := decypharr.Start(ctx); err != nil {
		log.Fatal(err)
	}

}
