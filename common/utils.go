package common

import (
	"bufio"
	"fmt"
	"log"
	"math/rand"
	"net/url"
	"os"
	"strings"
)

type Magnet struct {
	Name     string
	InfoHash string
	Size     int64
	Link     string
}

func OpenMagnetFile(filePath string) string {
	file, err := os.Open(filePath)
	if err != nil {
		log.Println("Error opening file:", err)
		return ""
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			return
		}
	}(file) // Ensure the file is closed after the function ends

	// Create a scanner to read the file line by line
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		magnetLink := scanner.Text()
		if magnetLink != "" {
			return magnetLink
		}
	}

	// Check for any errors during scanning
	if err := scanner.Err(); err != nil {
		log.Println("Error reading file:", err)
	}
	return ""
}

func GetMagnetInfo(magnetLink string) (*Magnet, error) {
	if magnetLink == "" {
		return nil, fmt.Errorf("error getting magnet from file")
	}
	magnetURI, err := url.Parse(magnetLink)
	if err != nil {
		return nil, fmt.Errorf("error parsing magnet link")
	}
	query := magnetURI.Query()
	xt := query.Get("xt")
	dn := query.Get("dn")

	// Extract BTIH
	parts := strings.Split(xt, ":")
	btih := ""
	if len(parts) > 2 {
		btih = parts[2]
	}
	magnet := &Magnet{
		InfoHash: btih,
		Name:     dn,
		Size:     0,
		Link:     magnetLink,
	}
	return magnet, nil
}

func RandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}
