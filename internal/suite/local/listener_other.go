//go:build !unix

package local

import "net"

func Listen(string, string) (net.Listener, func(), error) { return nil, nil, ErrTarget }
