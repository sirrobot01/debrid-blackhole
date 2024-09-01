package common

import (
	"bufio"
	"encoding/base32"
	"encoding/hex"
	"fmt"
	"github.com/anacrolix/torrent/metainfo"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"regexp"
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

func OpenMagnetHttpURL(magnetLink string) (*Magnet, error) {
	resp, err := http.Get(magnetLink)
	if err != nil {
		return nil, fmt.Errorf("error making GET request: %v", err)
	}
	defer func(resp *http.Response) {
		err := resp.Body.Close()
		if err != nil {
			return
		}
	}(resp) // Ensure the response is closed after the function ends

	// Create a scanner to read the file line by line

	mi, err := metainfo.Load(resp.Body)
	if err != nil {
		return nil, err
	}
	hash := mi.HashInfoBytes()
	infoHash := hash.HexString()
	info, err := mi.UnmarshalInfo()
	if err != nil {
		return nil, err
	}
	log.Println("InfoHash: ", infoHash)
	magnet := &Magnet{
		InfoHash: infoHash,
		Name:     info.Name,
		Size:     info.Length,
		Link:     mi.Magnet(&hash, &info).String(),
	}
	return magnet, nil

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

func ExtractInfoHash(magnetDesc string) string {
	const prefix = "xt=urn:btih:"
	start := strings.Index(magnetDesc, prefix)
	if start == -1 {
		return ""
	}
	hash := ""
	start += len(prefix)
	end := strings.IndexAny(magnetDesc[start:], "&#")
	if end == -1 {
		hash = magnetDesc[start:]
	} else {
		hash = magnetDesc[start : start+end]
	}
	hash, _ = processInfoHash(hash) // Convert to hex if needed
	return hash
}

func processInfoHash(input string) (string, error) {
	// Regular expression for a valid 40-character hex infohash
	hexRegex := regexp.MustCompile("^[0-9a-fA-F]{40}$")

	// If it's already a valid hex infohash, return it as is
	if hexRegex.MatchString(input) {
		return strings.ToLower(input), nil
	}

	// If it's 32 characters long, it might be Base32 encoded
	if len(input) == 32 {
		// Ensure the input is uppercase and remove any padding
		input = strings.ToUpper(strings.TrimRight(input, "="))

		// Try to decode from Base32
		decoded, err := base32.StdEncoding.DecodeString(input)
		if err == nil && len(decoded) == 20 {
			// If successful and the result is 20 bytes, encode to hex
			return hex.EncodeToString(decoded), nil
		}
	}

	// If we get here, it's not a valid infohash and we couldn't convert it
	return "", fmt.Errorf("invalid infohash: %s", input)
}
