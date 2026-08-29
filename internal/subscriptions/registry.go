package subscriptions

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

type Group string
type Parser string

const (
	Primary   Group = "primary"
	Emergency Group = "emergency"

	Standard    Parser = "standard"
	BlackTemple Parser = "blacktemple"
	Inline      Parser = "inline"
	WireGuard   Parser = "wireguard"
)

type ValidationError struct{ Code string }

func (err *ValidationError) Error() string { return err.Code }

type Subscription struct {
	ID               string  `json:"id"`
	Name             string  `json:"name"`
	GroupName        Group   `json:"group_name"`
	Parser           Parser  `json:"parser"`
	Secret           string  `json:"secret,omitempty"`
	SecretConfigured bool    `json:"secret_configured,omitempty"`
	Enabled          bool    `json:"enabled"`
	IntervalSeconds  int     `json:"interval_seconds"`
	LastAttempt      int64   `json:"last_attempt"`
	LastSuccess      int64   `json:"last_success"`
	LastStatus       string  `json:"last_status"`
	LastError        *string `json:"last_error"`
	LastLinks        int     `json:"last_links"`
	LastTested       int     `json:"last_tested"`
	LastAvailable    int     `json:"last_available"`
	CreatedAt        int64   `json:"created_at"`
	UpdatedAt        int64   `json:"updated_at"`
}

type BuiltinSource struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	URL             string `json:"url"`
	Repository      string `json:"repository"`
	Description     string `json:"description"`
	IntervalSeconds int    `json:"interval_seconds"`
}

var defaultEmergencySources = []BuiltinSource{
	{
		ID: "ebrasha-public", Name: "EbraSha — проверенный список",
		URL:         "https://raw.githubusercontent.com/ebrasha/free-v2ray-public-list/refs/heads/main/V2Ray-Config-By-EbraSha.txt",
		Repository:  "https://github.com/ebrasha/free-v2ray-public-list",
		Description: "Обновляемый универсальный список; уже использовался как аварийный источник.", IntervalSeconds: 3600,
	},
	{
		ID: "default-au1rxx", Name: "Au1rxx — универсальная подписка",
		URL:         "https://raw.githubusercontent.com/Au1rxx/free-vpn-subscriptions/main/output/v2ray-base64.txt",
		Repository:  "https://github.com/Au1rxx/free-vpn-subscriptions",
		Description: "Умеренный V2Ray/Base64-набор вместо крупных Clash и sing-box выгрузок.", IntervalSeconds: 3600,
	},
}

func DefaultEmergencySources() []BuiltinSource {
	result := make([]BuiltinSource, len(defaultEmergencySources))
	copy(result, defaultEmergencySources)
	return result
}

// MissingDefaults returns only records that need inserting. Existing records
// are never changed, so a user's enabled checkbox remains authoritative.
func MissingDefaults(existing []Subscription, defaultEnabled bool) []Subscription {
	known := make(map[string]bool, len(existing))
	for _, item := range existing {
		known[item.ID] = true
	}
	result := []Subscription{}
	for _, item := range defaultEmergencySources {
		if known[item.ID] {
			continue
		}
		result = append(result, Subscription{
			ID: item.ID, Name: item.Name, GroupName: Emergency, Parser: Standard,
			Secret: item.URL, Enabled: defaultEnabled, IntervalSeconds: item.IntervalSeconds,
		})
	}
	return result
}

// LegacyMigrationPlan mirrors the old subscriptions.env migration without
// coupling the portable core to a database or filesystem.
func LegacyMigrationPlan(existing []Subscription, environment map[string]string, defaultEnabled bool) []Subscription {
	if len(existing) != 0 {
		return MissingDefaults(existing, defaultEnabled)
	}
	result := []Subscription{}
	if secret := environment["new"]; secret != "" {
		result = append(result, Subscription{ID: "blacktemple-new", Name: "BlackTemple new", GroupName: Primary, Parser: BlackTemple, Secret: secret, Enabled: true, IntervalSeconds: envInterval(environment["new_interval"], 900)})
	}
	if secret := environment["legacy"]; secret != "" {
		result = append(result, Subscription{ID: "legacy-private", Name: "Legacy private", GroupName: Primary, Parser: Standard, Secret: secret, Enabled: true, IntervalSeconds: envInterval(environment["legacy_interval"], 900)})
	}
	result = append(result, MissingDefaults(result, defaultEnabled)...)
	return result
}

func envInterval(value string, fallback int) int {
	if parsed, err := strconv.Atoi(value); err == nil && value != "" {
		return parsed
	}
	return fallback
}

func ValidateFields(payload map[string]any, partial bool) (map[string]any, error) {
	if !partial {
		for _, key := range []string{"name", "group", "parser", "secret"} {
			if _, ok := payload[key]; !ok {
				return nil, &ValidationError{Code: "missing_" + key}
			}
		}
	}
	result := map[string]any{}
	if value, ok := payload["name"]; ok {
		name := strings.TrimSpace(valueString(value))
		if length := len([]rune(name)); length < 1 || length > 100 {
			return nil, &ValidationError{Code: "invalid_name"}
		}
		result["name"] = name
	}
	if value, ok := payload["group"]; ok {
		group := strings.ToLower(valueString(value))
		if group != string(Primary) && group != string(Emergency) {
			return nil, &ValidationError{Code: "invalid_group"}
		}
		result["group_name"] = group
	}
	if value, ok := payload["parser"]; ok {
		parser := strings.ToLower(valueString(value))
		if parser != string(Standard) && parser != string(BlackTemple) && parser != string(Inline) && parser != string(WireGuard) {
			return nil, &ValidationError{Code: "invalid_parser"}
		}
		result["parser"] = parser
	}
	if value, ok := payload["secret"]; ok {
		secret := strings.TrimSpace(valueString(value))
		if secret == "" || len([]byte(secret)) > 8<<20 {
			return nil, &ValidationError{Code: "invalid_secret"}
		}
		result["secret"] = secret
	}
	if value, ok := payload["enabled"]; ok {
		result["enabled"] = valueTruthy(value)
	}
	if value, ok := payload["interval_seconds"]; ok {
		interval, err := valueInt(value)
		if err != nil {
			return nil, &ValidationError{Code: "invalid_interval"}
		}
		if interval < 300 || interval > 604800 {
			return nil, &ValidationError{Code: "interval_out_of_range"}
		}
		result["interval_seconds"] = interval
	}
	return result, nil
}

func Decode(body []byte) []string {
	text := strings.TrimSpace(strings.ToValidUTF8(string(body), "�"))
	if configs := encodeWireGuardConfigs(text); len(configs) > 0 {
		return configs
	}
	if !strings.Contains(text, "://") {
		compact := strings.Join(strings.Fields(text), "")
		if decoded, err := decodeBase64(compact); err == nil {
			text = strings.ToValidUTF8(string(decoded), "�")
		}
	}
	result := []string{}
	for _, raw := range strings.Split(text, "\n") {
		line := strings.ReplaceAll(strings.TrimSpace(strings.TrimSuffix(raw, "\r")), "&amp;", "&")
		lower := strings.ToLower(line)
		for _, prefix := range []string{"vless://", "vmess://", "trojan://", "ss://", "hysteria2://", "hy2://", "wireguard://", "wg://", "amneziawg://", "awg://", "orcheroute://call/"} {
			if strings.HasPrefix(lower, prefix) {
				result = append(result, line)
				break
			}
		}
	}
	return result
}

func encodeWireGuardConfigs(text string) []string {
	if !strings.Contains(strings.ToLower(text), "[interface]") || !strings.Contains(strings.ToLower(text), "[peer]") {
		return nil
	}
	configs := []string{}
	current := []string{}
	flush := func() {
		normalized := normalizeWireGuardConfig(strings.Join(current, "\n"))
		if normalized == "" || !strings.Contains(strings.ToLower(normalized), "[peer]") {
			return
		}
		encoded := base64.RawURLEncoding.EncodeToString([]byte(normalized))
		configs = append(configs, "wireguard://"+encoded)
	}
	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if strings.EqualFold(line, "[Interface]") && len(current) > 0 {
			flush()
			current = nil
		}
		current = append(current, line)
	}
	flush()
	return unique(configs)
}

func normalizeWireGuardConfig(value string) string {
	result := []string{}
	section := ""
	for _, raw := range strings.Split(value, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			if section != "interface" && section != "peer" {
				continue
			}
			result = append(result, "["+section+"]")
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found || section == "" {
			continue
		}
		result = append(result, strings.ToLower(strings.TrimSpace(key))+"="+strings.TrimSpace(value))
	}
	return strings.Join(result, "\n")
}

func NormalizeInline(value string) (string, int) {
	decoded := Decode([]byte(value))
	uniqueLinks := unique(decoded)
	return strings.Join(uniqueLinks, "\n"), len(decoded) - len(uniqueLinks)
}

type Cache struct {
	UpdatedAt int64    `json:"updated_at"`
	Links     []string `json:"links"`
}

func NewCache(links []string, now time.Time) Cache {
	return Cache{UpdatedAt: now.Unix(), Links: unique(links)}
}

func ParseCache(payload []byte) []string {
	var cache Cache
	if json.Unmarshal(payload, &cache) != nil {
		return []string{}
	}
	result := []string{}
	for _, link := range cache.Links {
		if strings.Contains(link, "://") {
			result = append(result, link)
		}
	}
	return result
}

func RefreshDue(now time.Time, lastSuccess int64, intervalSeconds int, force bool, cachedLinks []string) bool {
	return force || now.Unix()-lastSuccess >= int64(intervalSeconds) || len(cachedLinks) == 0
}

// NextUpdate returns the first instant at which the subscription becomes due.
// Zero means that no honest date exists yet: the source is disabled, has never
// been fetched successfully, or has no valid interval.
func NextUpdate(lastSuccess int64, intervalSeconds int, enabled bool) int64 {
	if !enabled || lastSuccess <= 0 || intervalSeconds <= 0 {
		return 0
	}
	return lastSuccess + int64(intervalSeconds)
}

func decodeBase64(value string) ([]byte, error) {
	value = strings.TrimRight(value, "=")
	if decoded, err := base64.RawURLEncoding.DecodeString(value); err == nil {
		return decoded, nil
	}
	return base64.RawStdEncoding.DecodeString(value)
}

func unique(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func valueString(value any) string {
	switch current := value.(type) {
	case nil:
		return "None"
	case bool:
		if current {
			return "True"
		}
		return "False"
	case string:
		return current
	default:
		return fmt.Sprint(current)
	}
}

func valueTruthy(value any) bool {
	switch current := value.(type) {
	case nil:
		return false
	case bool:
		return current
	case string:
		return current != ""
	case float64:
		return current != 0
	case float32:
		return current != 0
	case int:
		return current != 0
	case []any:
		return len(current) != 0
	case map[string]any:
		return len(current) != 0
	default:
		return true
	}
}

func valueInt(value any) (int, error) {
	switch current := value.(type) {
	case bool:
		if current {
			return 1, nil
		}
		return 0, nil
	case float64:
		if math.IsNaN(current) || math.IsInf(current, 0) {
			return 0, fmt.Errorf("invalid integer")
		}
		return int(current), nil
	case string:
		return strconv.Atoi(strings.TrimSpace(current))
	case int:
		return current, nil
	default:
		return 0, fmt.Errorf("invalid integer")
	}
}
