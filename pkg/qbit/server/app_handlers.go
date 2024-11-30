package server

import (
	"goBlack/common"
	"goBlack/pkg/qbit/shared"
	"net/http"
	"path/filepath"
)

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte("v4.3.2"))
}

func (s *Server) handleWebAPIVersion(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte("2.7"))
}

func (s *Server) handlePreferences(w http.ResponseWriter, r *http.Request) {
	preferences := shared.NewAppPreferences()

	preferences.WebUiUsername = s.qbit.Username
	preferences.SavePath = s.qbit.DownloadFolder
	preferences.TempPath = filepath.Join(s.qbit.DownloadFolder, "temp")

	common.JSONResponse(w, preferences, http.StatusOK)
}

func (s *Server) handleBuildInfo(w http.ResponseWriter, r *http.Request) {
	res := shared.BuildInfo{
		Bitness:    64,
		Boost:      "1.75.0",
		Libtorrent: "1.2.11.0",
		Openssl:    "1.1.1i",
		Qt:         "5.15.2",
		Zlib:       "1.2.11",
	}
	common.JSONResponse(w, res, http.StatusOK)
}

func (s *Server) shutdown(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}
