package adaptertest

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mapseekai/ogc-api/examples/demo"
	"github.com/mapseekai/ogc-api/owscore"
	"github.com/mapseekai/ogc-api/response"
	"github.com/mapseekai/ogc-api/server"
	"github.com/mapseekai/ogc-api/wms"
	"github.com/mapseekai/ogc-api/wmts"
)

type failingReader struct{ sent bool }

func (r *failingReader) Read(p []byte) (int, error) {
	if r.sent {
		return 0, errors.New("fixture read failure")
	}
	r.sent = true
	return copy(p, "prefix"), nil
}

type deadlineReader struct{ ctx context.Context }

func (r deadlineReader) Read([]byte) (int, error) {
	<-r.ctx.Done()
	return 0, r.ctx.Err()
}

type repeatReader struct{}

func (repeatReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

// RunStreamFailures verifies real socket failures instead of buffered app.Test calls.
func RunStreamFailures(t *testing.T, start Factory) {
	for _, name := range []string{"short body", "read failure", "deadline", "client disconnect"} {
		t.Run(name, func(t *testing.T) {
			config, err := demo.Config("https://public.example.test")
			if err != nil {
				t.Fatal(err)
			}
			if name == "deadline" {
				config.Timeout = 100 * time.Millisecond
			}
			var closes atomic.Int32
			finished := make(chan server.Event, 1)
			config.Observer = func(event server.Event) {
				if event.Stage == "finish" {
					finished <- event
				}
			}
			config.Handlers.GetTile = func(ctx context.Context, _ *wmts.TileRequest) (response.Payload, error) {
				var reader io.Reader
				length := response.UnknownLength
				switch name {
				case "short body":
					reader = strings.NewReader("short")
					length = 20
				case "read failure":
					reader = &failingReader{}
				case "deadline":
					reader = deadlineReader{ctx: ctx}
				case "client disconnect":
					reader = io.LimitReader(repeatReader{}, 64<<20)
				}
				return response.Stream("image/png", &streamBody{reader: reader, ctx: ctx, closed: &closes}, length), nil
			}
			engine, err := server.New(config)
			if err != nil {
				t.Fatal(err)
			}
			base := start(t, engine)
			if name == "client disconnect" {
				disconnect(t, base)
			} else {
				client := &http.Client{Timeout: 5 * time.Second}
				result, requestErr := client.Get(base + "/wmts?" + TileQuery)
				if result != nil {
					data, readErr := io.ReadAll(result.Body)
					if err := result.Body.Close(); err != nil {
						t.Error(err)
					}
					if requestErr == nil && readErr == nil {
						t.Error("interrupted stream appeared successful")
					}
					if strings.Contains(string(data), "ExceptionReport") {
						t.Error("exception was appended after content")
					}
				} else if requestErr == nil {
					t.Error("missing stream response and error")
				}
			}
			select {
			case event := <-finished:
				if event.Error == nil {
					t.Error("stream failure was not observable")
				}
			case <-time.After(7 * time.Second):
				t.Fatal("stream resources did not finish")
			}
			if closes.Load() != 1 {
				t.Fatalf("stream closed %d times", closes.Load())
			}
		})
	}
}

func disconnect(t *testing.T, base string) {
	t.Helper()
	target, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	connection, err := net.DialTimeout("tcp", target.Host, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintf(connection, "GET /wmts?%s HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", TileQuery, target.Host); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(connection)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if line == "\r\n" {
			break
		}
	}
	if _, err := reader.ReadByte(); err != nil {
		t.Fatal(err)
	}
	if tcp, ok := connection.(*net.TCPConn); ok {
		if err := tcp.SetLinger(0); err != nil {
			t.Fatal(err)
		}
	}
}

func testBusinessTimeout(t *testing.T, start Factory) {
	for _, protocol := range []owscore.Protocol{owscore.WMS, owscore.WMTS} {
		t.Run(string(protocol), func(t *testing.T) {
			config, err := demo.Config("https://public.example.test")
			if err != nil {
				t.Fatal(err)
			}
			config.Timeout = 10 * time.Millisecond
			config.Handlers.GetTile = func(ctx context.Context, _ *wmts.TileRequest) (response.Payload, error) {
				<-ctx.Done()
				return response.Payload{}, ctx.Err()
			}
			config.Handlers.GetMap = func(ctx context.Context, _ *wms.MapRequest) (response.Payload, error) {
				<-ctx.Done()
				return response.Payload{}, ctx.Err()
			}
			engine, err := server.New(config)
			if err != nil {
				t.Fatal(err)
			}
			path := "/wmts?" + TileQuery
			root := "ExceptionReport"
			if protocol == owscore.WMS {
				path = "/wms?" + MapQuery("1.3.0")
				root = "ServiceExceptionReport"
			}
			result, data := request(t, &http.Client{Timeout: 5 * time.Second}, "GET", start(t, engine)+path)
			if result.StatusCode != 500 || !strings.Contains(string(data), root) || int64(len(data)) != result.ContentLength {
				t.Fatalf("incomplete timeout XML: status=%d length=%d bytes=%d", result.StatusCode, result.ContentLength, len(data))
			}
		})
	}
}
