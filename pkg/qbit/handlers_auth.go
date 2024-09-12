package qbit

import (
	"net/http"
	"time"
)

func (q *QBit) handleLogin(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		http.Error(w, "Failed to parse form data", http.StatusBadRequest)
		return
	}

	username := r.Form.Get("username")
	password := r.Form.Get("password")

	// In a real implementation, you'd verify credentials here
	// For this mock, we'll accept any non-empty username and password
	if username == "" || password == "" {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}
	if username != q.Username || password != q.Password {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	// Generate a new SID
	sid, err := generateSID()
	if err != nil {
		http.Error(w, "Failed to generate session ID", http.StatusInternalServerError)
		return
	}

	// Set the SID cookie
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    sid,
		Path:     "/",
		HttpOnly: true,
		MaxAge:   315360000,
	})

	// Store the session
	sessions.Store(sid, time.Now().Add(24*time.Hour))

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Ok."))
}
