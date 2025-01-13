package main

import (
	"context"
	"flag"
	"github.com/sirrobot01/debrid-blackhole/cmd"
	"github.com/sirrobot01/debrid-blackhole/common"
	"log"
)

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "config.json", "path to the config file")
	flag.Parse()

	// Load the config file
	conf, err := common.LoadConfig(configPath)
	common.CONFIG = conf
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	if err := cmd.Start(ctx, conf); err != nil {
		log.Fatal(err)
	}

}
