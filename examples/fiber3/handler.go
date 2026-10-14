package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/mapseekai/ogc-api/api"
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
		return sendPlan(c, plan)
	}
}

// apiHandler is private example glue for the resource-oriented engine.
func apiHandler(engine *api.Engine, scope string) fiber.Handler {
	return func(c fiber.Ctx) error {
		path, query, _ := strings.Cut(string(c.Request().Header.RequestURI()), "?")
		plan, err := engine.Handle(c.Context(), api.Input{
			Method: strings.Clone(c.Method()), EscapedPath: path, RawQuery: query,
			Accept: strings.Clone(c.Get("Accept")), IfNoneMatch: strings.Clone(c.Get("If-None-Match")),
			ContentType: strings.Clone(c.Get("Content-Type")), Prefer: strings.Clone(c.Get("Prefer")),
			Body: bytes.NewReader(c.Body()), Scope: scope,
		})
		if err != nil {
			return fiber.ErrInternalServerError
		}
		return sendPlan(c, plan)
	}
}

func sendPlan(c fiber.Ctx, plan *response.Plan) error {
	if plan.Length > int64(int(^uint(0)>>1)) {
		_ = plan.Finish(0, fmt.Errorf("response length exceeds Fiber's native integer range"))
		return fiber.ErrInternalServerError
	}
	c.Response().Header.SetNoDefaultContentType(true)
	for key, value := range plan.Headers {
		c.Set(key, value)
	}
	c.Status(plan.Status)
	if c.Method() == "HEAD" || plan.Status == 204 || plan.Status == 304 {
		c.Response().SkipBody = true
		_ = plan.Finish(0, nil)
		return nil
	}
	stream := &asyncBody{plan: plan}
	// fasthttp reads after the handler returns and reports completion through
	// CloseWithError. A handler-level defer must not close a transferred stream.
	err := c.SendStream(stream, int(plan.Length))
	if err != nil {
		_ = stream.CloseWithError(err)
	}
	return err
}

// Deliberately does not implement io.Closer: fasthttp calls both Close and
// CloseWithError when both exist, which would lose the actual write error.
type asyncBody struct {
	plan      *response.Plan
	readBytes int64
	readErr   error
}

func (b *asyncBody) Read(p []byte) (int, error) {
	// fasthttp recovers a read panic before its normal CloseWithError call.
	// Record failure and finish ownership here before rethrowing to the writer.
	defer func() {
		if caught := recover(); caught != nil {
			defer func() { panic(caught) }()
			_ = b.CloseWithError(fmt.Errorf("response reader panicked: %v", caught))
		}
	}()
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
