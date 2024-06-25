package web

import (
	"crypto/tls"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/urfave/cli/v2"
	"go-micro.org/v4"
	log "go-micro.org/v4/logger"
	"go-micro.org/v4/registry"
	maddr "go-micro.org/v4/util/addr"
	"go-micro.org/v4/util/backoff"
	mhttp "go-micro.org/v4/util/http"
	mnet "go-micro.org/v4/util/net"
	signalutil "go-micro.org/v4/util/signal"
	mls "go-micro.org/v4/util/tls"
)

type service struct {
	mux *http.ServeMux
	srv *registry.Service

	exit chan chan error
	ex   chan bool
	opts Options

	sync.RWMutex
	running bool
	static  bool
}

func newService(opts ...Option) Service {
	options := newOptions(opts...)
	s := &service{
		opts:   options,
		mux:    http.NewServeMux(),
		static: true,
		ex:     make(chan bool),
	}
	s.srv = s.genSrv()

	return s
}

func (s *service) genSrv() *registry.Service {
	var (
		host string
		port string
		err  error
	)

	logger := s.opts.Logger

	// default host:port
	if len(s.opts.Address) > 0 {
		host, port, err = net.SplitHostPort(s.opts.Address)
		if err != nil {
			logger.Log(log.FatalLevel, err)
		}
	}

	// check the advertise address first
	// if it exists then use it, otherwise
	// use the address
	if len(s.opts.Advertise) > 0 {
		host, port, err = net.SplitHostPort(s.opts.Advertise)
		if err != nil {
			logger.Log(log.FatalLevel, err)
		}
	}

	addr, err := maddr.Extract(host)
	if err != nil {
		logger.Log(log.FatalLevel, err)
	}

	if strings.Count(addr, ":") > 0 {
		addr = "[" + addr + "]"
	}

	return &registry.Service{
		Name:    s.opts.Name,
		Version: s.opts.Version,
		Nodes: []*registry.Node{{
			Id:       s.opts.Id,
			Address:  net.JoinHostPort(addr, port),
			Metadata: s.opts.Metadata,
		}},
	}
}

func (s *service) run() {
	s.RLock()
	if s.opts.RegisterInterval <= time.Duration(0) {
		s.RUnlock()
		return
	}

	t := time.NewTicker(s.opts.RegisterInterval)
	s.RUnlock()

	for {
		select {
		case <-t.C:
			s.register()
		case <-s.ex:
			t.Stop()
			return
		}
	}
}

func (s *service) register() error {
	s.Lock()
	defer s.Unlock()

	if s.srv == nil {
		return nil
	}

	logger := s.opts.Logger

	// default to service registry
	r := s.opts.Service.Client().Options().Registry
	// switch to option if specified
	if s.opts.Registry != nil {
		r = s.opts.Registry
	}

	// service node need modify, node address maybe changed
	srv := s.genSrv()
	srv.Endpoints = s.srv.Endpoints
	s.srv = srv

	// use RegisterCheck func before register
	if err := s.opts.RegisterCheck(s.opts.Context); err != nil {
		logger.Logf(log.ErrorLevel, "Server %s-%s register check error: %s", s.opts.Name, s.opts.Id, err)
		return err
	}

	var regErr error

	// try three times if necessary
	for i := 0; i < 3; i++ {
		// attempt to register
		if err := r.Register(s.srv, registry.RegisterTTL(s.opts.RegisterTTL)); err != nil {
			// set the error
			regErr = err
			// backoff then retry
			time.Sleep(backoff.Do(i + 1))

			continue
		}
		// success so nil error
		regErr = nil

		break
	}

	return regErr
}

func (s *service) deregister() error {
	s.Lock()
	defer s.Unlock()

	if s.srv == nil {
		return nil
	}
	// default to service registry
	r := s.opts.Service.Client().Options().Registry
	// switch to option if specified
	if s.opts.Registry != nil {
		r = s.opts.Registry
	}

	return r.Deregister(s.srv)
}

func (s *service) start() error {
	s.Lock()
	defer s.Unlock()

	if s.running {
		return nil
	}

	for _, fn := range s.opts.BeforeStart {
		if err := fn(); err != nil {
			return err
		}
	}

	listener, err := s.listen("tcp", s.opts.Address)
	if err != nil {
		return err
	}

	logger := s.opts.Logger

	s.opts.Address = listener.Addr().String()
	srv := s.genSrv()
	srv.Endpoints = s.srv.Endpoints
	s.srv = srv

	var handler http.Handler

	if s.opts.Handler != nil {
		handler = s.opts.Handler
	} else {
		handler = s.mux
		var r sync.Once

		// register the html dir
		r.Do(func() {
			// static dir
			static := s.opts.StaticDir
			if s.opts.StaticDir[0] != '/' {
				dir, _ := os.Getwd()
				static = filepath.Join(dir, static)
			}

			// set static if no / handler is registered
			if s.static {
				_, err := os.Stat(static)
				if err == nil {
					logger.Logf(log.InfoLevel, "Enabling static file serving from %s", static)
					s.mux.Handle("/", http.FileServer(http.Dir(static)))
				}
			}
		})
	}

	var httpSrv *http.Server
	if s.opts.Server != nil {
		httpSrv = s.opts.Server
	} else {
		httpSrv = &http.Server{}
	}

	httpSrv.Handler = handler

	go httpSrv.Serve(listener)

	for _, fn := range s.opts.AfterStart {
		if err := fn(); err != nil {
			return err
		}
	}

	s.exit = make(chan chan error, 1)
	s.running = true

	go func() {
		ch := <-s.exit
		ch <- listener.Close()
	}()

	logger.Logf(log.InfoLevel, "Listening on %v", listener.Addr().String())

	return nil
}

func (s *service) stop() error {
	s.Lock()
	defer s.Unlock()

	if !s.running {
		return nil
	}

	for _, fn := range s.opts.BeforeStop {
		if err := fn(); err != nil {
			return err
		}
	}

	ch := make(chan error, 1)
	s.exit <- ch
	s.running = false

	s.opts.Logger.Log(log.InfoLevel, "Stopping")

	for _, fn := range s.opts.AfterStop {
		if err := fn(); err != nil {
			if chErr := <-ch; chErr != nil {
				return chErr
			}

			return err
		}
	}

	return <-ch
}

func (s *service) Client() *http.Client {
	rt := mhttp.NewRoundTripper(
		mhttp.WithRegistry(s.opts.Registry),
	)
	return &http.Client{
		Transport: rt,
	}
}

func (s *service) Handle(pattern string, handler http.Handler) {
	var seen bool
	s.RLock()
	for _, ep := range s.srv.Endpoints {
		if ep.Name == pattern {
			seen = true
			break
		}
	}
	s.RUnlock()

	// if its unseen then add an endpoint
	if !seen {
		s.Lock()
		s.srv.Endpoints = append(s.srv.Endpoints, &registry.Endpoint{
			Name: pattern,
		})
		s.Unlock()
	}

	// disable static serving
	if pattern == "/" {
		s.Lock()
		s.static = false
		s.Unlock()
	}

	// register the handler
	s.mux.Handle(pattern, handler)
}

func (s *service) HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request)) {
	var seen bool

	s.RLock()
	for _, ep := range s.srv.Endpoints {
		if ep.Name == pattern {
			seen = true
			break
		}
	}
	s.RUnlock()

	if !seen {
		s.Lock()
		s.srv.Endpoints = append(s.srv.Endpoints, &registry.Endpoint{
			Name: pattern,
		})
		s.Unlock()
	}

	// disable static serving
	if pattern == "/" {
		s.Lock()
		s.static = false
		s.Unlock()
	}

	s.mux.HandleFunc(pattern, handler)
}

func (s *service) Init(opts ...Option) error {
	s.Lock()

	for _, o := range opts {
		o(&s.opts)
	}

	serviceOpts := []micro.Option{}

	if len(s.opts.Flags) > 0 {
		serviceOpts = append(serviceOpts, micro.Flags(s.opts.Flags...))
	}

	if s.opts.Registry != nil {
		serviceOpts = append(serviceOpts, micro.Registry(s.opts.Registry))
	}

	s.Unlock()

	serviceOpts = append(serviceOpts, micro.Action(func(ctx *cli.Context) error {
		s.Lock()
		defer s.Unlock()

		if ttl := ctx.Int("register_ttl"); ttl > 0 {
			s.opts.RegisterTTL = time.Duration(ttl) * time.Second
		}

		if interval := ctx.Int("register_interval"); interval > 0 {
			s.opts.RegisterInterval = time.Duration(interval) * time.Second
		}

		if name := ctx.String("server_name"); len(name) > 0 {
			s.opts.Name = name
		}

		if ver := ctx.String("server_version"); len(ver) > 0 {
			s.opts.Version = ver
		}

		if id := ctx.String("server_id"); len(id) > 0 {
			s.opts.Id = id
		}

		if addr := ctx.String("server_address"); len(addr) > 0 {
			s.opts.Address = addr
		}

		if adv := ctx.String("server_advertise"); len(adv) > 0 {
			s.opts.Advertise = adv
		}

		if s.opts.Action != nil {
			s.opts.Action(ctx)
		}

		return nil
	}))

	s.RLock()
	// pass in own name and version
	if s.opts.Service.Name() == "" {
		serviceOpts = append(serviceOpts, micro.Name(s.opts.Name))
	}

	serviceOpts = append(serviceOpts, micro.Version(s.opts.Version))

	s.RUnlock()

	s.opts.Service.Init(serviceOpts...)

	s.Lock()
	srv := s.genSrv()
	srv.Endpoints = s.srv.Endpoints
	s.srv = srv
	s.Unlock()

	return nil
}

func (s *service) Start() error {
	if err := s.start(); err != nil {
		return err
	}

	if err := s.register(); err != nil {
		return err
	}

	// start reg loop
	go s.run()

	return nil
}

func (s *service) Stop() error {
	// exit reg loop
	close(s.ex)

	if err := s.deregister(); err != nil {
		return err
	}

	return s.stop()
}

func (s *service) Run() error {
	if err := s.start(); err != nil {
		return err
	}

	logger := s.opts.Logger
	// start the profiler
	if s.opts.Service.Options().Profile != nil {
		// to view mutex contention
		runtime.SetMutexProfileFraction(5)
		// to view blocking profile
		runtime.SetBlockProfileRate(1)

		if err := s.opts.Service.Options().Profile.Start(); err != nil {
			return err
		}

		defer func() {
			if err := s.opts.Service.Options().Profile.Stop(); err != nil {
				logger.Log(log.ErrorLevel, err)
			}
		}()
	}

	if err := s.register(); err != nil {
		return err
	}

	// start reg loop
	go s.run()

	ch := make(chan os.Signal, 1)
	if s.opts.Signal {
		signal.Notify(ch, signalutil.Shutdown()...)
	}

	select {
	// wait on kill signal
	case sig := <-ch:
		logger.Logf(log.InfoLevel, "Received signal %s", sig)
	// wait on context cancel
	case <-s.opts.Context.Done():
		logger.Log(log.InfoLevel, "Received context shutdown")
	}

	// exit reg loop
	close(s.ex)

	if err := s.deregister(); err != nil {
		return err
	}

	return s.stop()
}

// Options returns the options for the given service.
func (s *service) Options() Options {
	return s.opts
}

func (s *service) listen(network, addr string) (net.Listener, error) {
	var (
		listener net.Listener
		err      error
	)

	// TODO: support use of listen options
	if s.opts.Secure || s.opts.TLSConfig != nil {
		config := s.opts.TLSConfig

		fn := func(addr string) (net.Listener, error) {
			if config == nil {
				hosts := []string{addr}

				// check if its a valid host:port
				if host, _, err := net.SplitHostPort(addr); err == nil {
					if len(host) == 0 {
						hosts = maddr.IPs()
					} else {
						hosts = []string{host}
					}
				}

				// generate a certificate
				cert, err := mls.Certificate(hosts...)
				if err != nil {
					return nil, err
				}
				config = &tls.Config{Certificates: []tls.Certificate{cert}}
			}

			return tls.Listen(network, addr, config)
		}

		listener, err = mnet.Listen(addr, fn)
	} else {
		fn := func(addr string) (net.Listener, error) {
			return net.Listen(network, addr)
		}

		listener, err = mnet.Listen(addr, fn)
	}

	if err != nil {
		return nil, err
	}

	return listener, nil
}
