package structs

type errorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type magnetInfo struct {
	Id             int    `json:"id"`
	Filename       string `json:"filename"`
	Size           int64  `json:"size"`
	Hash           string `json:"hash"`
	Status         string `json:"status"`
	StatusCode     int    `json:"statusCode"`
	UploadDate     int    `json:"uploadDate"`
	Downloaded     int64  `json:"downloaded"`
	Uploaded       int64  `json:"uploaded"`
	DownloadSpeed  int    `json:"downloadSpeed"`
	UploadSpeed    int    `json:"uploadSpeed"`
	Seeders        int    `json:"seeders"`
	CompletionDate int    `json:"completionDate"`
	Type           string `json:"type"`
	Notified       bool   `json:"notified"`
	Version        int    `json:"version"`
	NbLinks        int    `json:"nbLinks"`
	Files          []struct {
		Name string `json:"n"`
		Size int64  `json:"s"`
		Link string `json:"l"`
	} `json:"files"`
}

type AllDebridTorrentInfoResponse struct {
	Status string `json:"status"`
	Data   struct {
		Magnets magnetInfo `json:"magnets"`
	} `json:"data"`
	Error *errorResponse `json:"error"`
}

type AllDebridUploadMagnetResponse struct {
	Status string `json:"status"`
	Data   struct {
		Magnets []struct {
			Magnet           string `json:"magnet"`
			Hash             string `json:"hash"`
			Name             string `json:"name"`
			FilenameOriginal string `json:"filename_original"`
			Size             int64  `json:"size"`
			Ready            bool   `json:"ready"`
			ID               int    `json:"id"`
		} `json:"magnets"`
	}
	Error *errorResponse `json:"error"`
}

type AllDebridDownloadLink struct {
	Status string `json:"status"`
	Data   struct {
		Link      string        `json:"link"`
		Host      string        `json:"host"`
		Filename  string        `json:"filename"`
		Streaming []interface{} `json:"streaming"`
		Paws      bool          `json:"paws"`
		Filesize  int           `json:"filesize"`
		Id        string        `json:"id"`
		Path      []struct {
			Name string `json:"n"`
			Size int    `json:"s"`
		} `json:"path"`
	} `json:"data"`
	Error *errorResponse `json:"error"`
}
