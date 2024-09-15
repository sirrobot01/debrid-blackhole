package qbit

import (
	"net/http"
)

func (q *QBit) handleLogin(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Ok."))
}
