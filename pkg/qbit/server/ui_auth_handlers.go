package server

import (
	"github.com/sirrobot01/debrid-blackhole/internal/config"
	"golang.org/x/crypto/bcrypt"
	"net/http"
)

func (u *UIHandler) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if setup is needed
		cfg := config.GetConfig()
		if cfg.NeedsSetup() && r.URL.Path != "/setup" {
			http.Redirect(w, r, "/setup", http.StatusSeeOther)
			return
		}

		// Skip auth check for setup page
		if r.URL.Path == "/setup" {
			next.ServeHTTP(w, r)
			return
		}

		session, _ := store.Get(r, "auth-session")
		auth, ok := session.Values["authenticated"].(bool)

		if !ok || !auth {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (u *UIHandler) verifyAuth(username, password string) bool {
	// If you're storing hashed password, use bcrypt to compare
	if username == "" {
		return false
	}
	auth := config.GetConfig().GetAuth()
	if auth == nil {
		return false
	}
	if username != auth.Username {
		return false
	}
	err := bcrypt.CompareHashAndPassword([]byte(auth.Password), []byte(password))
	return err == nil
}
