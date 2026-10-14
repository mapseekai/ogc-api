package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestPublicationRetainsOldInFlightRouter(t *testing.T) {
	var host publication
	entered, unblock, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	host.publish(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-unblock
		_, _ = w.Write([]byte("old"))
	}))
	oldResponse := httptest.NewRecorder()
	go func() {
		defer close(finished)
		host.ServeHTTP(oldResponse, httptest.NewRequest("GET", "/wms", nil))
	}()
	<-entered
	drained := host.publish(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("new")) }))
	select {
	case <-drained:
		t.Error("retired provider while old response was active")
	default:
	}
	for _, path := range []string{"/wms", "/wmts", "/ogc/"} {
		recorder := httptest.NewRecorder()
		host.ServeHTTP(recorder, httptest.NewRequest("GET", path, nil))
		if recorder.Body.String() != "new" {
			t.Errorf("%s did not switch generation", path)
		}
	}
	close(unblock)
	<-finished
	select {
	case <-drained:
	case <-time.After(time.Second):
		t.Fatal("old generation never drained")
	}
	if oldResponse.Body.String() != "old" {
		t.Fatal("in-flight response changed its provider")
	}
}

func TestPublicationPanicAndShutdownReleaseReferences(t *testing.T) {
	var host publication
	host.publish(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("fixture") }))
	func() {
		defer func() {
			if recover() != "fixture" {
				t.Error("panic changed")
			}
		}()
		host.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/ogc/", nil))
	}()
	select {
	case <-host.publish(nil):
	case <-time.After(time.Second):
		t.Fatal("panic leaked generation")
	}
	recorder := httptest.NewRecorder()
	host.ServeHTTP(recorder, httptest.NewRequest("GET", "/ogc/", nil))
	if recorder.Code != 503 {
		t.Fatal(recorder.Code)
	}
}

func TestPublicationParallelReplacement(t *testing.T) {
	var host publication
	makeHandler := func(id int) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprintf(w, "%d", id) })
	}
	host.publish(makeHandler(0))
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				recorder := httptest.NewRecorder()
				host.ServeHTTP(recorder, httptest.NewRequest("GET", "/ogc/", nil))
				if recorder.Code != 200 || recorder.Body.Len() == 0 {
					t.Error("incomplete published response")
				}
			}
		}()
	}
	var retired []<-chan struct{}
	for id := range 100 {
		retired = append(retired, host.publish(makeHandler(id)))
	}
	wg.Wait()
	retired = append(retired, host.publish(nil))
	for _, drained := range retired {
		select {
		case <-drained:
		case <-time.After(time.Second):
			t.Fatal("retained retired generation")
		}
	}
}
