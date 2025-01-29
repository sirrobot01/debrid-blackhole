package commands

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/sirrobot01/debrid-blackhole/common"
	"github.com/sirrobot01/debrid-blackhole/pkg/cli"
)

func init() {
	cli.Register(cli.Command{
		Name:        "health",
		Description: "Check the health of the Blackhole server",
		Execute:     executeHealth,
	})
}

func executeHealth(args []string) error {
	fs := flag.NewFlagSet("health", flag.ExitOnError)
	configPath := fs.String("config", "/app/config.json", "path to config file")
	timeout := fs.Duration("timeout", 3*time.Second, "timeout for health check")

	if err := fs.Parse(args); err != nil {
		return err
	}

	port := "8282"
	if envPort := os.Getenv("QBIT_PORT"); envPort != "" {
		port = envPort
	} else {
		conf, err := common.LoadConfig(*configPath)
		if err == nil && conf.QBitTorrent.Port != "" {
			port = conf.QBitTorrent.Port
		}
	}

	url := fmt.Sprintf("http://localhost:%s/internal/version", port)

	client := &http.Client{
		Timeout: *timeout,
	}

	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check failed with status: %d", resp.StatusCode)
	}

	fmt.Println("Health check passed")
	return nil
}
