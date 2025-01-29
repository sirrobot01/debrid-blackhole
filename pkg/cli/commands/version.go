package commands

import (
	"encoding/json"
	"fmt"

	"github.com/sirrobot01/debrid-blackhole/pkg/cli"
	"github.com/sirrobot01/debrid-blackhole/pkg/version"
)

func init() {
    cli.Register(cli.Command{
        Name:        "version",
        Description: "Print version information",
        Execute:     executeVersion,
    })
}

func executeVersion(args []string) error {
    info := version.GetInfo()
    jsonOutput, err := json.MarshalIndent(info, "", "  ")
    if err != nil {
        return fmt.Errorf("failed to marshal version info: %w", err)
    }
    fmt.Println(string(jsonOutput))
    return nil
}
