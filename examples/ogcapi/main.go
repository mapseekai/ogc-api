// Command ogcapi demonstrates replaceable providers behind legacy and resource-oriented APIs.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mapseekai/ogc-api/api"
	"github.com/mapseekai/ogc-api/examples/demo"
	"github.com/mapseekai/ogc-api/examples/internal/nethttp"
	"github.com/mapseekai/ogc-api/owscore"
	"github.com/mapseekai/ogc-api/server"
)

func main() {
	address := demo.Address()
	publicURL := os.Getenv("OGC_PUBLIC_URL")
	if publicURL == "" {
		publicURL = "http://" + address
	}
	public, err := url.Parse(publicURL)
	if err != nil || public.Host == "" || public.Path != "" && public.Path != "/" || public.RawQuery != "" || public.Fragment != "" || public.User != nil {
		log.Fatal("OGC_PUBLIC_URL must be an absolute public HTTP(S) origin without path, query or credentials")
	}
	snapshot, legacyConfig, err := publications(publicURL)
	if err != nil {
		log.Fatal(err)
	}
	engine, err := api.New(api.Config{Catalog: api.Static(snapshot), MaxConcurrent: 256, Timeout: 20 * time.Second})
	if err != nil {
		log.Fatal(err)
	}
	legacyConfig.MaxConcurrent = 256
	legacy, err := server.New(legacyConfig)
	if err != nil {
		log.Fatal(err)
	}
	var current publication
	current.publish(combinedRouter(engine, legacy))
	httpServer := &http.Server{Addr: address, Handler: &current, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		deadline, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(deadline); err != nil {
			log.Printf("Shutdown: %v", err)
			_ = httpServer.Close()
		}
	}()
	log.Printf("OGC API example: %s/ogc/", publicURL)
	err = httpServer.ListenAndServe()
	stop()
	<-shutdownDone
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func combinedRouter(engine *api.Engine, legacy *server.Engine) *http.ServeMux {
	mux := http.NewServeMux()
	apiHandler := nethttp.APIHandler(engine, "public")
	mux.Handle("/ogc/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Expose-Headers", "Content-Crs, Content-Bbox, Content-Datetime, ETag")
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Prefer")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		apiHandler.ServeHTTP(w, r)
	}))
	mux.Handle("/wms", nethttp.Handler(legacy, server.Endpoint{Protocol: owscore.WMS}))
	mux.Handle("/wmts", nethttp.Handler(legacy, server.Endpoint{Protocol: owscore.WMTS}))
	mux.Handle("/", nethttp.Handler(legacy, server.Endpoint{Protocol: owscore.WMTS, Binding: owscore.REST}))
	return mux
}
