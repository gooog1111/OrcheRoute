package components

import (
	"encoding/binary"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"unicode"
)

type GeoSource struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	GeoIPURL    string `json:"geoip_url"`
	GeoSiteURL  string `json:"geosite_url"`
}

var GeoSources = []GeoSource{
	{ID: "metacubex", Name: "MetaCubeX", Description: "Основной набор Mihomo: страны, сервисы и категории доменов.", GeoIPURL: "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geoip.dat", GeoSiteURL: "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geosite.dat"},
	{ID: "runetfreedom", Name: "RunetFreedom", Description: "Российские блокировки: ru-blocked, antifilter, re:filter и общие категории.", GeoIPURL: "https://raw.githubusercontent.com/runetfreedom/russia-v2ray-rules-dat/release/geoip.dat", GeoSiteURL: "https://raw.githubusercontent.com/runetfreedom/russia-v2ray-rules-dat/release/geosite.dat"},
	{ID: "loyalsoldier", Name: "Loyalsoldier", Description: "Расширенный международный V2Ray-набор, совместимый с Mihomo.", GeoIPURL: "https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geoip.dat", GeoSiteURL: "https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geosite.dat"},
	{ID: "custom", Name: "Свои ссылки", Description: "Прямые HTTPS-ссылки на совместимые GeoIP.dat и GeoSite.dat."},
}

func ResolveGeoSource(id, geoIPURL, geoSiteURL string) (GeoSource, error) {
	id = strings.ToLower(strings.TrimSpace(id))
	if id == "" {
		id = "metacubex"
	}
	for _, source := range GeoSources {
		if source.ID != id {
			continue
		}
		if id == "custom" {
			source.GeoIPURL, source.GeoSiteURL = strings.TrimSpace(geoIPURL), strings.TrimSpace(geoSiteURL)
		}
		if err := validGeoURL(source.GeoIPURL); err != nil {
			return GeoSource{}, fmt.Errorf("invalid_geoip_url")
		}
		if err := validGeoURL(source.GeoSiteURL); err != nil {
			return GeoSource{}, fmt.Errorf("invalid_geosite_url")
		}
		return source, nil
	}
	return GeoSource{}, fmt.Errorf("unknown_geo_source")
}

func validGeoURL(raw string) error {
	if len(raw) > 2048 {
		return fmt.Errorf("url_too_long")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return fmt.Errorf("invalid_url")
	}
	return nil
}

func GeoCatalog(path string) ([]string, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	result := map[string]bool{}
	for offset := 0; offset < len(payload); {
		field, wire, next, ok := protobufField(payload, offset)
		if !ok {
			break
		}
		offset = next
		if field != 1 || wire != 2 {
			continue
		}
		length, size := binary.Uvarint(payload[offset:])
		if size <= 0 || length > uint64(len(payload)-offset-size) {
			break
		}
		entry := payload[offset+size : offset+size+int(length)]
		offset += size + int(length)
		if value := protobufStringField(entry, 1); validCategory(value) {
			result[strings.ToLower(value)] = true
		}
	}
	values := make([]string, 0, len(result))
	for value := range result {
		values = append(values, value)
	}
	sort.Strings(values)
	return values, nil
}

func protobufField(payload []byte, offset int) (int, int, int, bool) {
	key, size := binary.Uvarint(payload[offset:])
	if size <= 0 {
		return 0, 0, offset, false
	}
	field, wire, next := int(key>>3), int(key&7), offset+size
	switch wire {
	case 0:
		_, n := binary.Uvarint(payload[next:])
		if n <= 0 {
			return 0, 0, offset, false
		}
		return field, wire, next + n, true
	case 1:
		if next+8 > len(payload) {
			return 0, 0, offset, false
		}
		return field, wire, next + 8, true
	case 2:
		return field, wire, next, true
	case 5:
		if next+4 > len(payload) {
			return 0, 0, offset, false
		}
		return field, wire, next + 4, true
	default:
		return 0, 0, offset, false
	}
}

func protobufStringField(payload []byte, wanted int) string {
	for offset := 0; offset < len(payload); {
		field, wire, next, ok := protobufField(payload, offset)
		if !ok {
			return ""
		}
		offset = next
		if wire != 2 {
			continue
		}
		length, size := binary.Uvarint(payload[offset:])
		if size <= 0 || length > uint64(len(payload)-offset-size) {
			return ""
		}
		value := payload[offset+size : offset+size+int(length)]
		offset += size + int(length)
		if field == wanted {
			return string(value)
		}
	}
	return ""
}

func validCategory(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, current := range value {
		if !(unicode.IsLetter(current) || unicode.IsDigit(current) || strings.ContainsRune("-_@.!+", current)) {
			return false
		}
	}
	return true
}
