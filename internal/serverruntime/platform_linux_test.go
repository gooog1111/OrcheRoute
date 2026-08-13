//go:build linux

package serverruntime

import (
	"reflect"
	"testing"
)

func TestTransportSystemdStatePersistsAcrossBoot(t *testing.T) {
	if got, want := transportSystemctlArguments("orcheroute-core.service", true), []string{"enable", "--now", "orcheroute-routing.service", "orcheroute-core.service"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("enable arguments = %#v, want %#v", got, want)
	}
	if got, want := transportSystemctlArguments("orcheroute-core.service", false), []string{"disable", "--now", "orcheroute-core.service", "orcheroute-routing.service"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("disable arguments = %#v, want %#v", got, want)
	}
}
