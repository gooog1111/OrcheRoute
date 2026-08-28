// Package anet provides the small public interface expected by Pion transport.
// Modern Go implements Android network-interface enumeration without relying
// on private runtime symbols, so OrcheRoute deliberately delegates to net.
package anet

import "net"

func Interfaces() ([]net.Interface, error) { return net.Interfaces() }

func InterfaceAddrs() ([]net.Addr, error) { return net.InterfaceAddrs() }

func InterfaceByIndex(index int) (*net.Interface, error) { return net.InterfaceByIndex(index) }

func InterfaceByName(name string) (*net.Interface, error) { return net.InterfaceByName(name) }

func InterfaceAddrsByInterface(value *net.Interface) ([]net.Addr, error) {
	if value == nil {
		return nil, &net.OpError{Op: "route", Net: "ip+net", Err: errInvalidInterface{}}
	}
	return value.Addrs()
}

// SetAndroidVersion remains for source compatibility. No private net cache or
// Android-version-specific path is needed by the modern implementation.
func SetAndroidVersion(uint) {}

type errInvalidInterface struct{}

func (errInvalidInterface) Error() string { return "invalid network interface" }
