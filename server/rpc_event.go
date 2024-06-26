package server

import (
	"go-micro.org/v5/broker"
	"go-micro.org/v5/transport"
	"go-micro.org/v5/transport/headers"
)

// event is a broker event we handle on the server transport.
type event struct {
	err     error
	message *broker.Message
}

func (e *event) Ack() error {
	// there is no ack support
	return nil
}

func (e *event) Message() *broker.Message {
	return e.message
}

func (e *event) Error() error {
	return e.err
}

func (e *event) Topic() string {
	return e.message.Header[headers.Message]
}

func newEvent(msg transport.Message) *event {
	return &event{
		message: &broker.Message{
			Header: msg.Header,
			Body:   msg.Body,
		},
	}
}
