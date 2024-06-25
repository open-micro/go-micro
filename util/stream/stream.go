// Package stream encapsulates streams within streams
package stream

import (
	"context"
	"sync"

	"go-micro.org/v5/client"
	"go-micro.org/v5/codec"
	"go-micro.org/v5/metadata"
	"go-micro.org/v5/server"
)

type Stream interface {
	Context() context.Context
	SendMsg(interface{}) error
	RecvMsg(interface{}) error
	Close() error
}

type stream struct {
	Stream

	err     error
	request *request

	sync.RWMutex
}

type request struct {
	client.Request
	context context.Context
}

func (r *request) Codec() codec.Reader {
	return r.Request.Codec().(codec.Reader)
}

func (r *request) Header() map[string]string {
	md, _ := metadata.FromContext(r.context)
	return md
}

func (r *request) Read() ([]byte, error) {
	return nil, nil
}

func (s *stream) Request() server.Request {
	return s.request
}

func (s *stream) Send(v interface{}) error {
	err := s.Stream.SendMsg(v)
	if err != nil {
		s.Lock()
		s.err = err
		s.Unlock()
	}
	return err
}

func (s *stream) Recv(v interface{}) error {
	err := s.Stream.RecvMsg(v)
	if err != nil {
		s.Lock()
		s.err = err
		s.Unlock()
	}
	return err
}

func (s *stream) Error() error {
	s.RLock()
	defer s.RUnlock()
	return s.err
}

// New returns a new encapsulated stream
// Proto stream within a server.Stream.
func New(service, endpoint string, req interface{}, s Stream) server.Stream {
	return &stream{
		Stream: s,
		request: &request{
			context: s.Context(),
			Request: client.DefaultClient.NewRequest(service, endpoint, req),
		},
	}
}
