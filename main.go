package main

import (
	"context"
	"flag"
	"github.com/sirrobot01/debrid-blackhole/cmd"
	"github.com/sirrobot01/debrid-blackhole/internal/config"
	"log"
)

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "config.json", "path to the config file")
	flag.Parse()

	config.SetConfigPath(configPath)
	config.GetConfig()
	ctx := context.Background()
	if err := cmd.Start(ctx); err != nil {
		log.Fatal(err)
	}

}
