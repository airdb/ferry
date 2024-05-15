package main

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"text/template"

	"github.com/phuslu/log"
)

type HTTPWebFcgiHandler struct {
	Root       string
	DefaultApp string
	Pass       string
	KeepAlive  bool
	Param      string

	param *template.Template
	tp    *FcgiTransport
}

func (h *HTTPWebFcgiHandler) Load() (err error) {
	if h.Root == "" {
		return errors.New("empty fastcgi root")
	}

	if h.Pass == "" {
		return errors.New("empty fastcgi pass")
	}

	if h.param, err = template.New(h.Param).Parse(h.Param); err != nil {
		return err
	}

	network, address, found := strings.Cut(h.Pass, ":")
	if !found {
		return errors.New("fcgi pass error")
	}

	h.tp = &FcgiTransport{
		Root:      h.Root,
		SplitPath: []string{"*"},
		pool:      FcgiSharedPool(network, address, 5),

		serverSoftware: DefaultUserAgent,
	}

	return
}

func (h *HTTPWebFcgiHandler) Close() (err error) {
	return nil
}

// ServeHTTP handles the HTTP requests for the HTTPWebFcgiHandler.
// It sends the request to the FastCGI server and writes the response to the http.ResponseWriter.
//
// Parameters:
// - rw: The http.ResponseWriter used to write the response.
// - req: The http.Request representing the incoming request.
//
// Returns: None.
func (h *HTTPWebFcgiHandler) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	ri := req.Context().Value(RequestInfoContextKey).(*RequestInfo)

	log.Info().Context(ri.LogContext).Msg("web fcgi request")

	resp, err := h.tp.RoundTrip(req)
	if err != nil {
		log.Warn().Context(ri.LogContext).Err(err).Msg("round trip")
		http.Error(rw, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	rw.WriteHeader(resp.StatusCode)
	for k, vv := range resp.Header {
		for _, v := range vv {
			rw.Header().Add(k, v)
		}
	}
	// This code has a performance impact of reducing the performance by 90%.
	// rw.Header().Set("Connection", "close")

	transmitBytes, err := io.CopyBuffer(rw, NewRateLimitReader(resp.Body, 0), make([]byte, 2^15))
	log.Debug().Context(ri.LogContext).Int64("transmit_bytes", transmitBytes).Err(err).Msg("fcgi log")
}
