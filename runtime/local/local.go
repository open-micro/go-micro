// Package local provides a local runtime
package local

import (
	"go-micro.org/v5/runtime"
)

// NewRuntime returns a new local runtime.
func NewRuntime(opts ...runtime.Option) runtime.Runtime {
	return runtime.NewRuntime(opts...)
}
