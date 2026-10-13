package main

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/mapseekai/ogc-api/response"
	"github.com/mapseekai/ogc-api/server"
)

// ogcHandler is example-local glue; protocol handling stays in server.Engine.
func ogcHandler(engine *server.Engine, endpoint server.Endpoint) fiber.Handler {
	return func(c fiber.Ctx) error {
		// Copy the original URI before Fiber can reuse its request buffer.
		path, query, _ := strings.Cut(string(c.Request().Header.RequestURI()), "?")
		plan, err := engine.Handle(c.Context(), server.Input{
			Method: strings.Clone(c.Method()), RawQuery: query, EscapedPath: path,
			Protocol: endpoint.Protocol, Binding: endpoint.Binding, Scope: endpoint.Scope,
		})
		if err != nil {
			return fiber.ErrInternalServerError
		}
		if plan.Length > int64(int(^uint(0)>>1)) {
			_ = plan.Finish(0, fmt.Errorf("response length exceeds Fiber's native integer range"))
			return fiber.ErrInternalServerError
		}
		for key, value := range plan.Headers {
			c.Set(key, value)
		}
		c.Status(plan.Status)
		if c.Method() == "HEAD" {
			c.Response().SkipBody = true
			_ = plan.Finish(0, nil)
			return nil
		}
		stream := &asyncBody{plan: plan}
		// fasthttp consumes this reader after the handler returns. It calls
		// CloseWithError when writing ends; there must be no handler-level defer Close.
		err = c.SendStream(stream, int(plan.Length))
		if err != nil {
			_ = stream.CloseWithError(err)
		}
		return err
	}
}

// Deliberately does not implement io.Closer: fasthttp calls both Close and
// CloseWithError when both exist, which would lose the actual write error.
type asyncBody struct {
	plan      *response.Plan
	readBytes int64
	readErr   error
}

func (b *asyncBody) Read(p []byte) (int, error) {
	n, err := b.plan.Body.Read(p)
	b.readBytes += int64(n)
	if err != nil && err != io.EOF {
		b.readErr = err
	}
	return n, err
}
func (b *asyncBody) CloseWithError(err error) error {
	return b.plan.Finish(b.readBytes, errors.Join(b.readErr, err))
}
