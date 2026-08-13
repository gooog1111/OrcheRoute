package subscriptions

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const blackTempleSigningSecret = "Vy8Dpmn3wdMTaemIZiLDN8Knzw2cJaoNkcceOLooB3sPGtGkInMO8vBE1G/4AWKAq1t4BFFRY289aduMNxZr4A=="
const blackTempleAuthSecret = "MzkzMDk3ZTktOTFjMC00NzE1LWE5YTYtMjU1MjUxNzlmYjAw"

type BlackTempleFetcher struct {
	Client          *http.Client
	Bases           []string
	CredentialsPath string
	Now             func() time.Time
}

type blackTempleCredentials struct {
	AppID        string `json:"app_id"`
	HWID         string `json:"hwid"`
	Bearer       string `json:"bearer"`
	RefreshToken string `json:"refresh_token,omitempty"`
	AuthDomain   string `json:"auth_domain,omitempty"`
}

func (fetcher BlackTempleFetcher) Fetch(ctx context.Context, subscription Subscription) ([]string, error) {
	if subscription.Parser != BlackTemple {
		return nil, fmt.Errorf("unsupported_parser")
	}
	client := fetcher.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	bases := fetcher.Bases
	if len(bases) == 0 {
		bases = []string{"https://sub.argonaft1.online", "https://sub2.argonaft1.online"}
	}
	now := fetcher.Now
	if now == nil {
		now = time.Now
	}
	token := normalizeSubscriptionToken(subscription.Secret)
	if token == "" {
		return nil, fmt.Errorf("invalid_blacktemple_token")
	}
	credentials, _ := readBlackTempleCredentials(fetcher.CredentialsPath)
	if credentials == nil {
		var err error
		credentials, err = authenticateBlackTemple(ctx, client, bases, now)
		if err != nil {
			return nil, err
		}
		if err = writeBlackTempleCredentials(fetcher.CredentialsPath, *credentials); err != nil {
			return nil, err
		}
	}
	links, errors := collectBlackTemple(ctx, client, bases, token, *credentials, now)
	if len(links) == 0 {
		fresh, err := authenticateBlackTemple(ctx, client, bases, now)
		if err == nil {
			credentials = fresh
			_ = writeBlackTempleCredentials(fetcher.CredentialsPath, *credentials)
			links, errors = collectBlackTemple(ctx, client, bases, token, *credentials, now)
		}
	}
	if len(links) == 0 {
		return nil, fmt.Errorf("blacktemple_fetch_failed: %s", strings.Join(errors, ","))
	}
	return unique(links), nil
}

func normalizeSubscriptionToken(value string) string {
	value = strings.TrimSpace(value)
	if parsed, err := url.Parse(value); err == nil && parsed.Scheme != "" {
		for _, key := range []string{"token", "url", "link", "subscription"} {
			if nested := strings.TrimSpace(parsed.Query().Get(key)); nested != "" {
				return normalizeSubscriptionToken(nested)
			}
		}
		if parsed.Path != "" {
			parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
			if len(parts) > 0 {
				return parts[len(parts)-1]
			}
		}
		if strings.EqualFold(parsed.Scheme, "blacktemple") && parsed.Host != "" {
			return parsed.Host
		}
	}
	parts := strings.Split(strings.TrimRight(value, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

func authenticateBlackTemple(ctx context.Context, client *http.Client, bases []string, now func() time.Time) (*blackTempleCredentials, error) {
	appIDBytes := make([]byte, 16)
	hwidBytes := make([]byte, 16)
	if _, err := rand.Read(appIDBytes); err != nil {
		return nil, err
	}
	if _, err := rand.Read(hwidBytes); err != nil {
		return nil, err
	}
	appID := hex.EncodeToString(appIDBytes)
	hwidBytes[6] = (hwidBytes[6] & 0x0f) | 0x40
	hwidBytes[8] = (hwidBytes[8] & 0x3f) | 0x80
	hwid := fmt.Sprintf("%x-%x-%x-%x-%x", hwidBytes[0:4], hwidBytes[4:6], hwidBytes[6:8], hwidBytes[8:10], hwidBytes[10:16])
	errors := []string{}
	for _, base := range bases {
		path := "/api/auth"
		timestamp := fmt.Sprint(now().Unix())
		headers := map[string]string{"X-App-Id": appID, "X-Signature": blackTempleAuthSignature(path, timestamp, appID), "X-Timestamp": timestamp}
		var payload map[string]any
		if err := blackTempleJSON(ctx, client, base, path, headers, &payload); err != nil {
			errors = append(errors, err.Error())
			continue
		}
		bearer, _ := payload["jwt"].(string)
		confirm, _ := payload["confirm"].(string)
		if bearer == "" {
			errors = append(errors, base+": missing jwt")
			continue
		}
		return &blackTempleCredentials{AppID: appID, HWID: hwid, Bearer: bearer, RefreshToken: confirm, AuthDomain: base}, nil
	}
	return nil, fmt.Errorf("blacktemple_authentication_failed: %s", strings.Join(errors, ";"))
}

func collectBlackTemple(ctx context.Context, client *http.Client, bases []string, token string, credentials blackTempleCredentials, now func() time.Time) ([]string, []string) {
	links := []string{}
	errors := []string{}
	for _, base := range bases {
		refreshPath := "/api/refresh/" + token
		subPath := "/sub/" + token
		var ignored map[string]any
		if err := blackTempleSignedJSON(ctx, client, base, refreshPath, credentials, now, &ignored); err != nil {
			errors = append(errors, base+":refresh")
			continue
		}
		var payload map[string]any
		if err := blackTempleSignedJSON(ctx, client, base, subPath, credentials, now, &payload); err != nil {
			errors = append(errors, base+":sub")
			continue
		}
		if nested, ok := payload["sub"].(map[string]any); ok {
			payload = nested
		}
		servers, _ := payload["servers"].([]any)
		for _, raw := range servers {
			server, _ := raw.(map[string]any)
			if key, ok := server["key"].(string); ok && key != "" {
				links = append(links, key)
			}
		}
	}
	return links, errors
}

func blackTempleSignedJSON(ctx context.Context, client *http.Client, base, path string, credentials blackTempleCredentials, now func() time.Time, target any) error {
	timestamp := fmt.Sprint(now().Unix())
	headers := map[string]string{"Authorization": "Bearer " + credentials.Bearer, "X-App-Id": credentials.AppID, "X-Signature": blackTempleSignature("GET", path, timestamp, credentials.AppID), "X-Timestamp": timestamp, "X-hwid": credentials.HWID}
	return blackTempleJSON(ctx, client, base, path, headers, target)
}
func blackTempleJSON(ctx context.Context, client *http.Client, base, path string, headers map[string]string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+path, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", "BlackTemple/1.0.0")
	request.Header.Set("Accept", "application/json, text/plain, */*")
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("%s%s -> HTTP %d: %s", base, path, response.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(target)
}

func blackTempleSignature(method, path, timestamp, appID string) string {
	return hmacHex(blackTempleSigningSecret, method+"\n"+path+"\n"+timestamp+"\n"+appID)
}
func blackTempleAuthSignature(path, timestamp, appID string) string {
	return hmacHex(blackTempleAuthSecret, "GET\n"+path+"\n"+timestamp+"\n"+appID)
}
func hmacHex(secret, message string) string {
	value := hmac.New(sha256.New, []byte(secret))
	_, _ = value.Write([]byte(message))
	return hex.EncodeToString(value.Sum(nil))
}

func readBlackTempleCredentials(path string) (*blackTempleCredentials, error) {
	if path == "" {
		return nil, os.ErrNotExist
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var result blackTempleCredentials
	if json.Unmarshal(payload, &result) != nil || result.AppID == "" || result.HWID == "" || result.Bearer == "" {
		return nil, fmt.Errorf("invalid blacktemple credentials")
	}
	return &result, nil
}
func writeBlackTempleCredentials(path string, value blackTempleCredentials) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	temporary, err := os.CreateTemp(filepath.Dir(path), ".credentials-*.tmp")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
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
	return os.Rename(name, path)
}
