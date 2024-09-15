package qbit

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"github.com/go-chi/chi/v5"
	"net/http"
	"strings"
)

func (q *QBit) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		if subtle.ConstantTimeCompare([]byte(user), []byte(q.Username)) != 1 || subtle.ConstantTimeCompare([]byte(pass), []byte(q.Password)) != 1 {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func DecodeAuthHeader(header string) (string, string, error) {
	encodedTokens := strings.Split(header, " ")
	if len(encodedTokens) != 2 {
		return "", "", nil
	}
	encodedToken := encodedTokens[1]

	bytes, err := base64.StdEncoding.DecodeString(encodedToken)
	if err != nil {
		return "", "", err
	}

	bearer := string(bytes)

	colonIndex := strings.LastIndex(bearer, ":")
	host := bearer[:colonIndex]
	token := bearer[colonIndex+1:]

	return host, token, nil
}

func (q *QBit) authContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, token, err := DecodeAuthHeader(r.Header.Get("Authorization"))
		ctx := r.Context()
		if err == nil {
			ctx = context.WithValue(r.Context(), "host", host)
			ctx = context.WithValue(ctx, "token", token)
			q.arrs[host] = token
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func HashesCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_hashes := chi.URLParam(r, "hashes")
		var hashes []string
		if _hashes != "" {
			hashes = strings.Split(_hashes, "|")
		}
		if hashes == nil {
			// Get hashes from form
			_ = r.ParseForm()
			hashes = r.Form["hashes"]
		}
		ctx := context.WithValue(r.Context(), "hashes", hashes)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
