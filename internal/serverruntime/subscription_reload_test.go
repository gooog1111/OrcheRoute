package serverruntime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"

	"github.com/gooog1111/orcheroute/internal/subscriptions"
)

func TestSubscriptionProviderGroupsResolveSelectedSource(t *testing.T) {
	runtime := cleanTestRuntime(t)
	if _, err := runtime.Store.Create(context.Background(), subscriptions.Subscription{
		ID: "source-a", Name: "A", GroupName: subscriptions.Primary,
		Parser: subscriptions.Standard, Secret: "https://example.test", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if got := runtime.subscriptionProviderGroups([]string{"source-a"}, nil); !reflect.DeepEqual(got, []string{"primary"}) {
		t.Fatalf("groups=%#v", got)
	}
	if got := runtime.subscriptionProviderGroups(nil, nil); !reflect.DeepEqual(got, []string{"primary", "emergency"}) {
		t.Fatalf("all groups=%#v", got)
	}
}

func TestReloadSubscriptionProvidersAppliesEveryUpdatedPool(t *testing.T) {
	runtime := cleanTestRuntime(t)
	var lock sync.Mutex
	paths := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		lock.Lock()
		paths = append(paths, request.Method+" "+request.URL.Path)
		lock.Unlock()
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	runtime.Config.MihomoAPI = server.URL
	runtime.reloadSubscriptionProviders([]string{"primary", "emergency"})
	lock.Lock()
	defer lock.Unlock()
	want := []string{"PUT /providers/proxies/primary", "PUT /providers/proxies/emergency"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("reload paths=%#v", paths)
	}
}
