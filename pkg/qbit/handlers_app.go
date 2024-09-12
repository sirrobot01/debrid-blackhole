package qbit

import (
	"net/http"
	"path/filepath"
)

func (q *QBit) handleVersion(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte("v4.3.2"))
	w.WriteHeader(http.StatusOK)
}

func (q *QBit) handleWebAPIVersion(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte("2.7"))
	w.WriteHeader(http.StatusOK)
}

func (q *QBit) handlePreferences(w http.ResponseWriter, r *http.Request) {
	preferences := NewAppPreferences()

	preferences.WebUiUsername = q.Username
	preferences.SavePath = q.DownloadFolder
	preferences.TempPath = filepath.Join(q.DownloadFolder, "temp")

	JSONResponse(w, preferences, http.StatusOK)
}

func (q *QBit) handleBuildInfo(w http.ResponseWriter, r *http.Request) {
	res := BuildInfo{
		Bitness:    64,
		Boost:      "1.75.0",
		Libtorrent: "1.2.11.0",
		Openssl:    "1.1.1i",
		Qt:         "5.15.2",
		Zlib:       "1.2.11",
	}
	JSONResponse(w, res, http.StatusOK)
}

func (q *QBit) shutdown(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}
