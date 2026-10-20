//go:build !linux

package logexport

import "os"

func inheritOwner(_ *os.Root, _ string) error { return nil }
