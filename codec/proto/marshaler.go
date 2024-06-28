package proto

import (
	"go-micro.org/v5/codec"
	"google.golang.org/protobuf/proto"
)

type Marshaler struct{}

func (Marshaler) Marshal(v interface{}) ([]byte, error) {
	pb, ok := v.(proto.Message)
	if !ok {
		return nil, codec.ErrInvalidMessage
	}

	buf, err := proto.Marshal(pb)
	if err != nil {
		return nil, err
	}

	return buf, nil
}

func (Marshaler) Unmarshal(data []byte, v interface{}) error {
	pb, ok := v.(proto.Message)
	if !ok {
		return codec.ErrInvalidMessage
	}

	return proto.Unmarshal(data, pb)
}

func (Marshaler) String() string {
	return "proto"
}
