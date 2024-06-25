// Package api provides an http-rpc handler which provides the entire http request over rpc
package api

import (
	"net/http"

	"go-micro.org/v4/api/handler"
	api "go-micro.org/v4/api/proto"
	"go-micro.org/v4/api/router"
	"go-micro.org/v4/client"
	"go-micro.org/v4/errors"
	"go-micro.org/v4/selector"
	"go-micro.org/v4/util/ctx"
)

type apiHandler struct {
	opts handler.Options
}

const (
	// Handler is the name of the Handler.
	Handler = "api"
)

// API handler is the default handler which takes api.Request and returns api.Response.
func (a *apiHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	bsize := handler.DefaultMaxRecvSize
	if a.opts.MaxRecvSize > 0 {
		bsize = a.opts.MaxRecvSize
	}

	r.Body = http.MaxBytesReader(w, r.Body, bsize)

	request, err := requestToProto(r)
	if err != nil {
		er := errors.InternalServerError("go.micro.api", err.Error())

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(er.Error()))

		return
	}

	var service *router.Route

	if a.opts.Router != nil {
		// try get service from router
		s, err := a.opts.Router.Route(r)
		if err != nil {
			er := errors.InternalServerError("go.micro.api", err.Error())

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(er.Error()))

			return
		}

		service = s
	} else {
		// we have no way of routing the request
		er := errors.InternalServerError("go.micro.api", "no route found")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(er.Error()))

		return
	}

	// create request and response
	c := a.opts.Client
	req := c.NewRequest(service.Service, service.Endpoint.Name, request)
	rsp := &api.Response{}

	// create the context from headers
	cx := ctx.FromRequest(r)
	// create strategy:
	so := selector.WithStrategy(strategy(service.Versions))

	if err := c.Call(cx, req, rsp, client.WithSelectOption(so)); err != nil {
		w.Header().Set("Content-Type", "application/json")

		ce := errors.Parse(err.Error())
		switch ce.Code {
		case 0:
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(int(ce.Code))
		}

		w.Write([]byte(ce.Error()))

		return
	} else if rsp.StatusCode == 0 {
		rsp.StatusCode = http.StatusOK
	}

	for _, header := range rsp.GetHeader() {
		for _, val := range header.Values {
			w.Header().Add(header.Key, val)
		}
	}

	if len(w.Header().Get("Content-Type")) == 0 {
		w.Header().Set("Content-Type", "application/json")
	}

	w.WriteHeader(int(rsp.StatusCode))

	w.Write([]byte(rsp.Body))
}

func (a *apiHandler) String() string {
	return "api"
}

// NewHandler returns an api.Handler.
func NewHandler(opts ...handler.Option) handler.Handler {
	options := handler.NewOptions(opts...)

	return &apiHandler{
		opts: options,
	}
}
