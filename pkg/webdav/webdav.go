package webdav

import (
	"context"
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/sirrobot01/debrid-blackhole/pkg/service"
	"html/template"
	"net/http"
	"sync"
)

type WebDav struct {
	Handlers []*Handler
	ready    chan struct{}
}

func New() *WebDav {
	svc := service.GetService()
	w := &WebDav{
		Handlers: make([]*Handler, 0),
		ready:    make(chan struct{}),
	}
	for name, c := range svc.Debrid.Caches {
		h := NewHandler(name, c, c.GetLogger())
		w.Handlers = append(w.Handlers, h)
	}
	return w
}

func (wd *WebDav) Routes() http.Handler {
	chi.RegisterMethod("PROPFIND")
	chi.RegisterMethod("PROPPATCH")
	chi.RegisterMethod("MKCOL")
	chi.RegisterMethod("COPY")
	chi.RegisterMethod("MOVE")
	chi.RegisterMethod("LOCK")
	chi.RegisterMethod("UNLOCK")
	wr := chi.NewRouter()
	wr.Use(wd.commonMiddleware)

	// Create a readiness check middleware
	readinessMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-wd.ready:
				// WebDAV is ready, proceed
				next.ServeHTTP(w, r)
			default:
				// WebDAV is still initializing
				w.Header().Set("Retry-After", "10")
				http.Error(w, "WebDAV service is initializing, please try again shortly", http.StatusServiceUnavailable)
			}
		})
	}
	wr.Use(readinessMiddleware)

	wd.setupRootHandler(wr)
	wd.mountHandlers(wr)

	return wr
}

func (wd *WebDav) Start(ctx context.Context) error {
	wg := sync.WaitGroup{}
	errChan := make(chan error, len(wd.Handlers))

	for _, h := range wd.Handlers {
		wg.Add(1)
		go func(h *Handler) {
			defer wg.Done()
			if err := h.cache.Start(); err != nil {
				select {
				case errChan <- err:
				default:
				}
			}
		}(h)
	}

	// Use a separate goroutine to close channel after WaitGroup
	go func() {
		wg.Wait()
		close(errChan)

		// Signal that WebDAV is ready
		close(wd.ready)
	}()

	// Collect all errors
	var errors []error
	for err := range errChan {
		if err != nil {
			errors = append(errors, err)
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("multiple handlers failed: %v", errors)
	}
	return nil
}

func (wd *WebDav) mountHandlers(r chi.Router) {
	for _, h := range wd.Handlers {
		r.Mount(h.RootPath, h)
	}
}

func (wd *WebDav) setupRootHandler(r chi.Router) {
	r.Get("/", wd.handleRoot())
}

func (wd *WebDav) commonMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("DAV", "1, 2")
		w.Header().Set("Allow", "OPTIONS, PROPFIND, GET, HEAD, POST, PUT, DELETE, MKCOL, PROPPATCH, COPY, MOVE, LOCK, UNLOCK")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "OPTIONS, PROPFIND, GET, HEAD, POST, PUT, DELETE, MKCOL, PROPPATCH, COPY, MOVE, LOCK, UNLOCK")
		w.Header().Set("Access-Control-Allow-Headers", "Depth, Content-Type, Authorization")

		next.ServeHTTP(w, r)
	})
}

func (wd *WebDav) handleRoot() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")

		tmpl, err := template.New("root").Parse(rootTemplate)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		data := struct {
			Handlers []*Handler
			Prefix   string
		}{
			Handlers: wd.Handlers,
			Prefix:   "/webdav",
		}
		if err := tmpl.Execute(w, data); err != nil {
			return
		}
	}
}
