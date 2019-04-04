package sentry

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"time"

	"github.com/google/uuid"
)

const SdkName string = "sentry.go"
const SdkVersion string = "0.0.0-beta"
const SdkUserAgent string = SdkName + "/" + SdkVersion

type LogEntry struct {
	Message string
	Params  []interface{}
}

type Request struct{}

type Exception struct{}

type Context string

type Event struct {
	EventID     uuid.UUID              `json:"event_id"`
	Level       Level                  `json:"level"`
	Message     string                 `json:"message"`
	Fingerprint []string               `json:"fingerprint"`
	Timestamp   int64                  `json:"timestamp"`
	User        User                   `json:"user"`
	Breadcrumbs []*Breadcrumb          `json:"breadcrumbs"`
	Tags        map[string]string      `json:"tags"`
	Extra       map[string]interface{} `json:"extra"` // TODO: Should it be more strict?
	Sdk         ClientSdkInfo          `json:"sdk"`
	Transaction string                 `json:"transaction"`
	ServerName  string                 `json:"server_name"`
	Release     string                 `json:"release"`
	Dist        string                 `json:"dist"`
	Environment string                 `json:"environment"`
	// Logentry    LogEntry
	// Logger      string
	// Modules     map[string]string
	// Platform    string
	// Request     Request
	// Contexts    map[string]Context
	// Exception   []Exception
}

type ClientSdkInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type ClientOptions struct {
	Dsn         string
	Debug       bool
	SampleRate  float32
	BeforeSend  func(event *Event) *Event
	Transport   Transport
	ServerName  string
	Release     string
	Dist        string
	Environment string
}

type Clienter interface {
	AddBreadcrumb(breadcrumb *Breadcrumb, scope Scoper)
	CaptureMessage(message string, scope Scoper)
	CaptureException(exception error, scope Scoper)
	CaptureEvent(event *Event, scope Scoper)
	Recover(recoveredErr interface{}, scope *Scope)
	RecoverWithContext(ctx context.Context, recoveredErr interface{}, scope *Scope)
}

type Client struct {
	Options   ClientOptions
	Dsn       *Dsn
	Transport Transport
}

func NewClient(options ClientOptions) (*Client, error) {
	if options.Debug {
		debugger.Enable()
	}

	dsn, err := NewDsn(options.Dsn)

	if err != nil {
		return nil, err
	}

	if dsn == nil {
		debugger.Println("Sentry client initialized with an empty DSN")
	}

	transport := options.Transport
	if transport == nil {
		transport = new(HTTPTransport)
	}

	transport.Configure(options)

	return &Client{
		Options:   options,
		Dsn:       dsn,
		Transport: transport,
	}, nil
}

func (client *Client) AddBreadcrumb(breadcrumb *Breadcrumb, scope Scoper) {
	scope.AddBreadcrumb(breadcrumb)
}

func (client *Client) CaptureMessage(message string, scope Scoper) {
	event := client.eventFromMessage(message)
	client.CaptureEvent(event, scope)
}

func (client *Client) CaptureException(exception error, scope Scoper) {
	event := client.eventFromException(exception)
	client.CaptureEvent(event, scope)
}

func (client *Client) CaptureEvent(event *Event, scope Scoper) {
	if _, err := client.processEvent(event, scope); err != nil {
		debugger.Println(err)
	}
	log.Println("TODO[CaptureEvent]: Handle return values")
}

func (client *Client) Recover(recoveredErr interface{}, scope *Scope) {
	if recoveredErr == nil {
		recoveredErr = recover()
	}

	if recoveredErr != nil {
		if err, ok := recoveredErr.(error); ok {
			CaptureException(err)
		}

		if err, ok := recoveredErr.(string); ok {
			CaptureMessage(err)
		}
	}
}

func (client *Client) RecoverWithContext(ctx context.Context, recoveredErr interface{}, scope *Scope) {
	if recoveredErr == nil {
		recoveredErr = recover()
	}

	if recoveredErr != nil {
		var currentHub *Hub

		if HasHubOnContext(ctx) {
			currentHub = GetHubFromContext(ctx)
		} else {
			hub, err := GetCurrentHub()
			if err != nil {
				debugger.Println("Unable to retrieve Hub inside RecoverWithContext method;", err)
				return
			}
			currentHub = hub
		}

		if err, ok := recoveredErr.(error); ok {
			currentHub.CaptureException(err)
		}

		if err, ok := recoveredErr.(string); ok {
			currentHub.CaptureMessage(err)
		}
	}
}

func (client *Client) eventFromMessage(message string) *Event {
	return &Event{
		Message: message,
	}
}

func (client *Client) eventFromException(exception error) *Event {
	log.Println("TODO[CaptureException]: Extract stacktrace from the exception")
	return &Event{
		Message: exception.Error(),
	}
}

// TODO: Should return some sort of SentryResponse instead of http.Response
func (client *Client) processEvent(event *Event, scope Scoper) (*http.Response, error) {
	options := client.Options

	// TODO: Reconsider if its worth going away from default implementation
	// of other SDKs. In Go zero value (default) for float32 is 0.0,
	// which means that if someone uses ClientOptions{} struct directly
	// and we would not check for 0 here, we'd skip all events by default
	if options.SampleRate != 0.0 {
		randomFloat := rand.New(rand.NewSource(time.Now().UnixNano())).Float32()
		if randomFloat > options.SampleRate {
			return nil, fmt.Errorf("event dropped due to SampleRate hit")
		}
	}

	if event = client.prepareEvent(event, scope); event == nil {
		return nil, fmt.Errorf("event dropped by one of the EventProcessors")
	}

	if options.BeforeSend != nil {
		if event = options.BeforeSend(event); event == nil {
			return nil, fmt.Errorf("event dropped due to BeforeSend callback")
		}
	}

	return client.Transport.SendEvent(event)
}

func (client *Client) prepareEvent(event *Event, scope Scoper) *Event {
	// TODO: Set all the defaults, clear unnecessary stuff etc. here

	var emptyEventID uuid.UUID
	if event.EventID == emptyEventID {
		event.EventID = uuid.New()
	}

	if event.Timestamp == 0 {
		event.Timestamp = time.Now().Unix()
	}

	if event.Level == "" {
		event.Level = LevelInfo
	}

	event.Sdk = ClientSdkInfo{
		Name:    SdkName,
		Version: SdkVersion,
	}

	event.Transaction = "Don't sneak into my computer please"

	return scope.ApplyToEvent(event)
}