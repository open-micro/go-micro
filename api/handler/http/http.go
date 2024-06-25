// Package http is a http reverse proxy handler
package http

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"

	"go-micro.org/v5/api/handler"
	"go-micro.org/v5/api/router"
	"go-micro.org/v5/selector"
)

const (
	// Handler is the name of the handler.
	Handler = "http"
)

type httpHandler struct {
	options handler.Options
}

func (h *httpHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	service, err := h.getService(r)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if len(service) == 0 {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	rp, err := url.Parse(service)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	httputil.NewSingleHostReverseProxy(rp).ServeHTTP(w, r)
}

// getService returns the service for this request from the selector.
func (h *httpHandler) getService(r *http.Request) (string, error) {
	var service *router.Route

	if h.options.Router != nil {
		// try get service from router
		s, err := h.options.Router.Route(r)
		if err != nil {
			return "", err
		}

		service = s
	} else {
		// we have no way of routing the request
		return "", errors.New("no route found")
	}

	// create a random selector
	next := selector.Random(service.Versions)

	// get the next node
	s, err := next()
	if err != nil {
		return "", nil
	}

	return fmt.Sprintf("http://%s", s.Address), nil
}

func (h *httpHandler) String() string {
	return "http"
}

// NewHandler returns a http proxy handler.
func NewHandler(opts ...handler.Option) handler.Handler {
	options := handler.NewOptions(opts...)

	return &httpHandler{
		options: options,
	}
}
