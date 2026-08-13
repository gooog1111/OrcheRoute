package subscriptions

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

var cacheIDRE = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)

type FileCache struct{ Directory string }

type OverlayCache struct {
	Primary  CacheRepository
	Fallback CacheRepository
}

func (cache OverlayCache) Read(ctx context.Context, id string) ([]string, error) {
	links, err := cache.Primary.Read(ctx, id)
	if err != nil {
		return nil, err
	}
	if len(links) > 0 || cache.Fallback == nil {
		return links, nil
	}
	return cache.Fallback.Read(ctx, id)
}
func (cache OverlayCache) Write(ctx context.Context, id string, value Cache) error {
	return cache.Primary.Write(ctx, id, value)
}
func (cache OverlayCache) Remove(ctx context.Context, id string) error {
	return cache.Primary.Remove(ctx, id)
}

func (cache FileCache) path(id string) (string, error) {
	if !cacheIDRE.MatchString(id) {
		return "", fmt.Errorf("invalid subscription id")
	}
	return filepath.Join(cache.Directory, id+".json"), nil
}

func (cache FileCache) Read(_ context.Context, id string) ([]string, error) {
	path, err := cache.path(id)
	if err != nil {
		return nil, err
	}
	payload, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	return ParseCache(payload), nil
}

func (cache FileCache) Write(_ context.Context, id string, value Cache) error {
	path, err := cache.path(id)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cache.Directory, 0o700); err != nil {
		return err
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	temporary, err := os.CreateTemp(cache.Directory, ".cache-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(payload); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func (cache FileCache) Remove(_ context.Context, id string) error {
	path, err := cache.path(id)
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
