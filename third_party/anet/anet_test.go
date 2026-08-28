package anet

import "testing"

func TestInterfaceAdapterUsesPublicNetAPI(t *testing.T) {
	interfaces, err := Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	for index := range interfaces {
		if _, err := InterfaceAddrsByInterface(&interfaces[index]); err != nil {
			t.Fatalf("interface %q: %v", interfaces[index].Name, err)
		}
	}
	if _, err := InterfaceAddrsByInterface(nil); err == nil {
		t.Fatal("nil interface was accepted")
	}
}
