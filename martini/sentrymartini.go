package sentrymartini

import (
	"context"
	"net/http"
	"time"

	"github.com/go-martini/martini"

	"github.com/getsentry/sentry-go"
)

type handler struct {
	repanic         bool
	waitForDelivery bool
	timeout         time.Duration
}

type Options struct {
	// Repanic configures whether Sentry should repanic after recovery, in most cases it should be set to true,
	// as martini.Classic includes it's own Recovery middleware what handles http responses.
	Repanic bool
	// WaitForDelivery configures whether you want to block the request before moving forward with the response.
	// Because Martini's default `Recovery` handler doesn't restart the application,
	// it's safe to either skip this option or set it to `false`.
	WaitForDelivery bool
	// Timeout for the event delivery requests.
	Timeout time.Duration
}

// New returns a function that satisfies martini.Handler interface
// It can be used with New(), Use() or Handlers() methods.
func New(options Options) martini.Handler {
	handler := handler{
		repanic:         false,
		timeout:         time.Second * 2,
		waitForDelivery: false,
	}

	if options.Repanic {
		handler.repanic = true
	}

	if options.WaitForDelivery {
		handler.waitForDelivery = true
	}

	return handler.handle()
}

func (h *handler) handle() martini.Handler {
	return func(rw http.ResponseWriter, r *http.Request, c martini.Context) {
		hub := sentry.CurrentHub().Clone()
		c.Map(hub)
		defer h.recoverWithSentry(hub, r)
		c.Next()
	}
}

func (h *handler) recoverWithSentry(hub *sentry.Hub, r *http.Request) {
	if err := recover(); err != nil {
		hub.Scope().SetRequest(sentry.Request{}.FromHTTPRequest(r))
		eventID := hub.RecoverWithContext(
			context.WithValue(r.Context(), sentry.RequestContextKey, r),
			err,
		)
		if eventID != nil && h.waitForDelivery {
			hub.Flush(h.timeout)
		}
		if h.repanic {
			panic(err)
		}
	}
}
