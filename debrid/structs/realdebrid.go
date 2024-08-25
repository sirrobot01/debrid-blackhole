package structs

type RealDebridAvailabilityResponse map[string]Hosters

type Hosters map[string][]FileIDs

type FileIDs map[string]FileVariant

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
	Bytes            int    `json:"bytes"`
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
