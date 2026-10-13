// Package nethttp contains HTTP glue shared only by the repository examples.
package nethttp

import (
	"errors"
	"io"
	"net/http"

	"github.com/mapseekai/ogc-api/server"
)

func Handler(engine *server.Engine, endpoint server.Endpoint) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		plan, err := engine.Handle(r.Context(), server.Input{
			Method: r.Method, RawQuery: r.URL.RawQuery, EscapedPath: r.URL.EscapedPath(),
			Protocol: endpoint.Protocol, Binding: endpoint.Binding, Scope: endpoint.Scope,
		})
		if err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		defer func() {
			// Finish is idempotent; this fallback also cleans up a panicking Reader.
			_ = plan.Finish(0, errors.New("transport interrupted before completion"))
		}()
		for key, value := range plan.Headers {
			w.Header().Set(key, value)
		}
		w.WriteHeader(plan.Status)
		if r.Method == http.MethodHead {
			_ = plan.Finish(0, nil) // Cleanup errors are delivered to the observer.
			return
		}
		n, sendErr := io.Copy(w, plan.Body)
		if err := plan.Finish(n, sendErr); err != nil {
			// Headers are committed: abort the transfer instead of appending XML.
			panic(http.ErrAbortHandler)
		}
	}
}
