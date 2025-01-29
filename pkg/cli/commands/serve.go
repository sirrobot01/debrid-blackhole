package commands

import (
	"context"
	"flag"
	"fmt"

	"github.com/sirrobot01/debrid-blackhole/cmd"
	"github.com/sirrobot01/debrid-blackhole/common"
	"github.com/sirrobot01/debrid-blackhole/pkg/cli"
)

func init() {
    cli.Register(cli.Command{
        Name:        "serve",
        Description: "Start the Blackhole server",
        Execute:     executeServe,
    })
}

func executeServe(args []string) error {
    fs := flag.NewFlagSet("serve", flag.ExitOnError)
    configFile := fs.String("config", "config.json", "path to config file")

    if err := fs.Parse(args); err != nil {
        return err
    }

    conf, err := common.LoadConfig(*configFile)
    if err != nil {
        return fmt.Errorf("failed to load config: %w", err)
    }

    common.CONFIG = conf
    return cmd.Start(context.Background(), conf)
}
