//go:build !windows

package divert

import "errors"

var errUnsupported = errors.New("WinDivert is only available on Windows")

type Handle struct{}

func Open(dllPath, source string, sniff bool) (*Handle, error) { return nil, errUnsupported }
func OpenGuard(dllPath, source string) (*Handle, error)        { return nil, errUnsupported }
func (*Handle) Receive(buffer []byte) (int, Metadata, error)   { return 0, Metadata{}, errUnsupported }
func (*Handle) Send(packet []byte, ifIndex uint32) error       { return errUnsupported }
func (*Handle) Close() error                                   { return nil }
