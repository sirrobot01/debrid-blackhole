package server

import (
	"context"
	"encoding/base64"
	"github.com/go-chi/chi/v5"
	"goBlack/pkg/arr"
	"net/http"
	"strings"
)

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

func (s *Server) CategoryContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		category := strings.Trim(r.URL.Query().Get("category"), "")
		if category == "" {
			// Get from form
			_ = r.ParseForm()
			category = r.Form.Get("category")
			if category == "" {
				// Get from multipart form
				_ = r.ParseMultipartForm(0)
				category = r.FormValue("category")
			}
		}
		ctx := r.Context()
		ctx = context.WithValue(r.Context(), "category", category)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) authContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, token, err := DecodeAuthHeader(r.Header.Get("Authorization"))
		category := r.Context().Value("category").(string)
		a := &arr.Arr{
			Name: category,
		}
		if err == nil {
			a.Host = host
			a.Token = token
		}
		s.qbit.Arrs.AddOrUpdate(a)
		ctx := context.WithValue(r.Context(), "arr", a)
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
