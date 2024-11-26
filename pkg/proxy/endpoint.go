package proxy

import (
	"bytes"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/stripe/stripe-cli/pkg/websocket"
)

//
// Public types
//

// EndpointConfig contains the optional configuration parameters of an EndpointClient.
type EndpointConfig struct {
	HTTPClient *http.Client

	Log *log.Logger

	ResponseHandler EndpointResponseHandler

	// OutCh is the channel to send data and statuses to for processing in other packages
	OutCh chan websocket.IElement
}

// EndpointResponseHandler handles a response from the endpoint.
type EndpointResponseHandler interface {
	ProcessResponse(eventContext, string, *http.Response)
}

// EndpointResponseHandlerFunc is an adapter to allow the use of ordinary
// functions as response handlers. If f is a function with the
// appropriate signature, ResponseHandler(f) is a
// ResponseHandler that calls f.
type EndpointResponseHandlerFunc func(eventContext, string, *http.Response)

// ProcessResponse calls f(evtCtx, forwardURL, resp).
func (f EndpointResponseHandlerFunc) ProcessResponse(evtCtx eventContext, forwardURL string, resp *http.Response) {
	f(evtCtx, forwardURL, resp)
}

// FailedToPostError describes a failure to send a POST request to an endpoint
type FailedToPostError struct {
	Err error
}

func (f FailedToPostError) Error() string {
	return f.Err.Error()
}

// EndpointClient is the client used to POST webhook requests to the local endpoint.
type EndpointClient struct {
	// URL the client sends POST requests to
	URL string

	headers map[string]string

	connect bool

	events map[string]bool

	// Optional configuration parameters
	cfg *EndpointConfig

	isEventDestination bool
}

// SupportsEventType takes an event of a webhook and compares it to the internal
// list of supported events
func (c *EndpointClient) SupportsEventType(connect bool, eventType string) bool {
	if connect != c.connect {
		return false
	}

	// Endpoint supports all events, always return true
	if c.events["*"] || c.events[eventType] {
		return true
	}

	return false
}

// SupportsContext takes the context string of an event, and determines whether the endpoint supports
// this context
func (c *EndpointClient) SupportsContext(context string) bool {
	if c.connect {
		return context != ""
	}

	return context == ""
}

// Post sends a message to the local endpoint.
func (c *EndpointClient) Post(evtCtx eventContext) error {
	c.cfg.Log.WithFields(log.Fields{
		"prefix": "proxy.EndpointClient.Post",
	}).Debug("Forwarding event to local endpoint")

	req, err := http.NewRequest(http.MethodPost, c.URL, bytes.NewBuffer([]byte(evtCtx.requestBody)))
	if err != nil {
		return err
	}

	for k, v := range evtCtx.requestHeaders {
		req.Header.Add(k, v)
	}

	// add custom headers
	for k, v := range c.headers {
		if strings.ToLower(k) == "host" {
			req.Host = v
		} else {
			req.Header.Add(k, v)
		}
	}

	resp, err := c.cfg.HTTPClient.Do(req)
	if err != nil {
		c.cfg.OutCh <- websocket.ErrorElement{
			Error: FailedToPostError{Err: err},
		}
		return err
	}

	defer resp.Body.Close()

	c.cfg.ResponseHandler.ProcessResponse(evtCtx, c.URL, resp)

	return nil
}

// PostV2 sends a message to a local event destination
func (c *EndpointClient) PostV2(evtCtx eventContext) error {
	req, err := http.NewRequest(http.MethodPost, c.URL, bytes.NewBuffer([]byte(evtCtx.requestBody)))
	if err != nil {
		return err
	}

	for k, v := range evtCtx.requestHeaders {
		req.Header.Add(k, v)
	}

	// add custom headers
	for k, v := range c.headers {
		if strings.ToLower(k) == "host" {
			req.Host = v
		} else {
			req.Header.Add(k, v)
		}
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.cfg.OutCh <- websocket.ErrorElement{
			Error: FailedToPostError{Err: err},
		}
		return err
	}
	defer resp.Body.Close()

	c.cfg.ResponseHandler.ProcessResponse(evtCtx, c.URL, resp)

	return nil
}

//
// Public functions
//

// NewEndpointClient returns a new EndpointClient.
func NewEndpointClient(url string, headers []string, connect bool, events []string, isEventDestination bool, cfg *EndpointConfig) *EndpointClient {
	if cfg == nil {
		cfg = &EndpointConfig{}
	}

	if cfg.Log == nil {
		cfg.Log = &log.Logger{Out: io.Discard}
	}

	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
			Timeout: defaultTimeout,
		}
	}

	if cfg.ResponseHandler == nil {
		cfg.ResponseHandler = EndpointResponseHandlerFunc(func(eventContext, string, *http.Response) {})
	}

	return &EndpointClient{
		URL:                url,
		headers:            convertToMapAndSanitize(headers),
		connect:            connect,
		events:             convertToMap(events),
		isEventDestination: isEventDestination,
		cfg:                cfg,
	}
}

//
// Private constants
//

const (
	defaultTimeout = 30 * time.Second
)

//
// Private functions
//

func convertToMap(events []string) map[string]bool {
	eventsMap := make(map[string]bool)
	for _, event := range events {
		eventsMap[event] = true
	}

	return eventsMap
}

func convertToMapAndSanitize(headers []string) map[string]string {
	reg := regexp.MustCompile("[\x00-\x1f]+")

	headerMap := make(map[string]string)

	for _, header := range headers {
		header = reg.ReplaceAllString(header, "")

		splitHeader := strings.SplitN(header, ":", 2)
		headerKey := strings.TrimSpace(splitHeader[0])
		headerVal := strings.TrimSpace(splitHeader[1])

		if headerKey != "" {
			headerMap[headerKey] = headerVal
		}
	}

	return headerMap
}
