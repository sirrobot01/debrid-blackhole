package server

import "net/http"

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte("Ok."))
}
