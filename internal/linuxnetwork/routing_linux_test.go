//go:build linux

package linuxnetwork

import "testing"

func TestRoutingTableMissing(t *testing.T) {
	for _, message := range []string{
		"Error: ipv4: FIB table does not exist.\nFlush terminated",
		"routing table does not exist",
		"RTNETLINK answers: No such file or directory",
	} {
		if !routingTableMissing(message) {
			t.Fatalf("missing table was treated as fatal: %q", message)
		}
	}
	if routingTableMissing("RTNETLINK answers: Operation not permitted") {
		t.Fatal("permission error must remain fatal")
	}
}
