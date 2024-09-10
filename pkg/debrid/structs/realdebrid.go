package structs

import (
	"encoding/json"
	"fmt"
)

type RealDebridAvailabilityResponse map[string]Hoster

func (r *RealDebridAvailabilityResponse) UnmarshalJSON(data []byte) error {
	// First, try to unmarshal as an object
	var objectData map[string]Hoster
	err := json.Unmarshal(data, &objectData)
	if err == nil {
		*r = objectData
		return nil
	}

	// If that fails, try to unmarshal as an array
	var arrayData []map[string]Hoster
	err = json.Unmarshal(data, &arrayData)
	if err != nil {
		return fmt.Errorf("failed to unmarshal as both object and array: %v", err)
	}

	// If it's an array, use the first element
	if len(arrayData) > 0 {
		*r = arrayData[0]
		return nil
	}

	// If it's an empty array, initialize as an empty map
	*r = make(map[string]Hoster)
	return nil
}

type Hoster struct {
	Rd []map[string]FileVariant `json:"rd"`
}

func (h *Hoster) UnmarshalJSON(data []byte) error {
	// Attempt to unmarshal into the expected structure (an object with an "rd" key)
	type Alias Hoster
	var obj Alias
	if err := json.Unmarshal(data, &obj); err == nil {
		*h = Hoster(obj)
		return nil
	}

	// If unmarshalling into an object fails, check if it's an empty array
	var arr []interface{}
	if err := json.Unmarshal(data, &arr); err == nil && len(arr) == 0 {
		// It's an empty array; initialize with no entries
		*h = Hoster{Rd: nil}
		return nil
	}

	// If both attempts fail, return an error
	return fmt.Errorf("hoster: cannot unmarshal JSON data: %s", string(data))
}

type FileVariant struct {
	Filename string `json:"filename"`
	Filesize int    `json:"filesize"`
}

type RealDebridAddMagnetSchema struct {
	Id  string `json:"id"`
	Uri string `json:"uri"`
}

type RealDebridTorrentInfo struct {
	ID               string `json:"id"`
	Filename         string `json:"filename"`
	OriginalFilename string `json:"original_filename"`
	Hash             string `json:"hash"`
	Bytes            int64  `json:"bytes"`
	OriginalBytes    int    `json:"original_bytes"`
	Host             string `json:"host"`
	Split            int    `json:"split"`
	Progress         int    `json:"progress"`
	Status           string `json:"status"`
	Added            string `json:"added"`
	Files            []struct {
		ID       int    `json:"id"`
		Path     string `json:"path"`
		Bytes    int    `json:"bytes"`
		Selected int    `json:"selected"`
	} `json:"files"`
	Links   []string `json:"links"`
	Ended   string   `json:"ended,omitempty"`
	Speed   int      `json:"speed,omitempty"`
	Seeders int      `json:"seeders,omitempty"`
}

// 5e6e2e77fd3921a7903a41336c844cc409bf8788/14527C07BDFDDFC642963238BB6E7507B9742947/66A1CD1A5C7F4014877A51AC2620E857E3BB4D16
