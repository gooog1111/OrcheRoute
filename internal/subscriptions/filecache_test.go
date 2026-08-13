package subscriptions

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestFileCacheRoundTripAndReplace(t *testing.T) {
	cache := FileCache{Directory: filepath.Join(t.TempDir(), "cache")}
	ctx := context.Background()
	if err := cache.Write(ctx, "sub-one", NewCache([]string{"vless://one", "vless://one"}, time.Unix(1, 0))); err != nil {
		t.Fatal(err)
	}
	if err := cache.Write(ctx, "sub-one", NewCache([]string{"trojan://two"}, time.Unix(2, 0))); err != nil {
		t.Fatal(err)
	}
	links, err := cache.Read(ctx, "sub-one")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(links, []string{"trojan://two"}) {
		t.Fatalf("unexpected links: %#v", links)
	}
	if err := cache.Remove(ctx, "sub-one"); err != nil {
		t.Fatal(err)
	}
	links, err = cache.Read(ctx, "sub-one")
	if err != nil || len(links) != 0 {
		t.Fatalf("unexpected removed cache: %#v %v", links, err)
	}
}

func TestFileCacheRejectsTraversal(t *testing.T) {
	cache := FileCache{Directory: t.TempDir()}
	if err := cache.Write(context.Background(), "../secret", NewCache(nil, time.Now())); err == nil {
		t.Fatal("traversal accepted")
	}
}

func TestOverlayCacheReadsFallbackAndWritesPrimary(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	primary := FileCache{Directory: filepath.Join(root, "primary")}
	fallback := FileCache{Directory: filepath.Join(root, "fallback")}
	overlay := OverlayCache{Primary: primary, Fallback: fallback}
	if err := fallback.Write(ctx, "sub", NewCache([]string{"vless://fallback"}, time.Unix(1, 0))); err != nil {
		t.Fatal(err)
	}
	links, err := overlay.Read(ctx, "sub")
	if err != nil || !reflect.DeepEqual(links, []string{"vless://fallback"}) {
		t.Fatalf("fallback: %#v %v", links, err)
	}
	if err := overlay.Write(ctx, "sub", NewCache([]string{"vless://primary"}, time.Unix(2, 0))); err != nil {
		t.Fatal(err)
	}
	links, err = overlay.Read(ctx, "sub")
	if err != nil || !reflect.DeepEqual(links, []string{"vless://primary"}) {
		t.Fatalf("primary: %#v %v", links, err)
	}
}
