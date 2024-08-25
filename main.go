package main

import (
	"flag"
	"goBlack/cmd"
	"goBlack/common"
	"log"
)

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "config.json", "path to the config file")
	flag.Parse()

	// Load the config file
	conf, err := common.LoadConfig(configPath)
	if err != nil {
		log.Fatal(err)
	}
	cmd.Start(conf)

}
