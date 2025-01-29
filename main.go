package main

import (
	"fmt"
	"os"

	"github.com/sirrobot01/debrid-blackhole/pkg/cli"
	_ "github.com/sirrobot01/debrid-blackhole/pkg/cli/commands"
)

func main() {
	if err := cli.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
