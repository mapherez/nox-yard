//go:build linux

package logexport

import (
	"os"
	"syscall"
)

func inheritOwner(root *os.Root, name string) error {
	info, err := root.Stat(".")
	if err != nil {
		return err
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	return root.Chown(name, int(owner.Uid), int(owner.Gid))
}
