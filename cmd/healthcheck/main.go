package main

import (
	"cmp"
	"net/http"
	"os"
)

func main() {
	port := cmp.Or(os.Getenv("QBIT_PORT"), "8282")
	resp, err := http.Get("http://localhost:" + port + "/api/v2/app/version")
	if err != nil {
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		os.Exit(1)
	}

	os.Exit(0)
}
