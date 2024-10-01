package qbit

import (
	"net/http"
)

func (q *QBit) handleLogin(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte("Ok."))
}
