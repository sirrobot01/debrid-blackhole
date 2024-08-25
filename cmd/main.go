package cmd

import (
	"goBlack/common"
	"goBlack/debrid"
	"log"
)

func Start(config *common.Config) {

	log.Print("[*] BlackHole running")
	deb := debrid.NewDebrid(config.Debrid)
	if config.Proxy.Enabled {
		go StartProxy(config, deb)
	}
	StartBlackhole(config, deb)

}
