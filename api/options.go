package api

import (
	"go-micro.org/v5/api/router"
	registry2 "go-micro.org/v5/api/router/registry"
	"go-micro.org/v5/client"
	"go-micro.org/v5/registry"
)

func NewOptions(opts ...Option) Options {
	options := Options{
		Address: ":8080",
	}

	for _, o := range opts {
		o(&options)
	}

	return options
}

// WithAddress sets the address to listen
func WithAddress(addr string) Option {
	return func(o *Options) error {
		o.Address = addr
		return nil
	}
}

// WithRouter sets the router to use e.g static or registry.
func WithRouter(r router.Router) Option {
	return func(o *Options) error {
		o.Router = r
		return nil
	}
}

// WithRegistry sets the api's client and router to use registry.
func WithRegistry(r registry.Registry) Option {
	return func(o *Options) error {
		o.Client = client.NewClient(client.Registry(r))
		o.Router = registry2.NewRouter(router.WithRegistry(r))
		return nil
	}
}
