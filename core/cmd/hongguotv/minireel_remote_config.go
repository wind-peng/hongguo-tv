package main

// Remote configuration for the MiniReel playback source chain.
//
// Hongguo's official API changes without warning: playback endpoints get
// discontinued (multi_video_model/v1/ was shut down in favour of
// video_model/v1/) and device identities get silently blacklisted. These
// values used to be baked into the binary, so every official change forced
// a new app release and every installed TV had to reinstall.
//
// remote-config.json (hosted in this repo, fetched at startup) overrides the
// baked-in playback endpoints, API hosts and protocol version / channel /
// device-model strings. A maintainer edits that one JSON file; installed TVs
// pick it up without reinstalling.
//
// Safety rules, deliberately boring:
//   - The fetch has a short timeout and runs in the background. It never
//     blocks startup or playback.
//   - Any failure (no network, timeout, bad JSON, invalid URL) is ignored
//     silently: the baked-in values keep working.
//   - The last good config is cached in the data dir, so playback works
//     offline too.
//   - A config can only override endpoints/hosts/protocol strings. A change
//     to the request SIGNING algorithm itself still needs a code update.
//   - The existing multi-endpoint fallback and identity re-registration are
//     untouched; remote config only changes which values they try.
//   - The account/passport signing profile is intentionally NOT configurable
//     here and stays baked in.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// miniReelRemoteConfigURL is a var (not const) so tests can point it at a
// local test server.
var miniReelRemoteConfigURL = "https://raw.githubusercontent.com/wind-peng/hongguo-tv/main/remote-config.json"

const miniReelRemoteConfigTimeout = 5 * time.Second
const miniReelRemoteConfigMaxBytes = 64 << 10

// miniReelProtocolProfile is one device/app identity used for device_register
// and playback signing on a single playback route.
type miniReelProtocolProfile struct {
	VersionCode        string `json:"version_code"`
	VersionName        string `json:"version_name"`
	Channel            string `json:"channel"`
	AID                string `json:"aid"`
	DeviceType         string `json:"device_type"`
	DeviceBrand        string `json:"device_brand"`
	DeviceManufacturer string `json:"device_manufacturer"`
	OSAPI              string `json:"os_api"`
	OSVersion          string `json:"os_version"`
	Resolution         string `json:"resolution"`
	DPI                string `json:"dpi"`
}

type miniReelRemoteConfig struct {
	VideoTabEndpoint           string                  `json:"video_tab_endpoint"`
	LegacyAPIEndpoint          string                  `json:"legacy_api_endpoint"`
	LegacyEndpoints            []string                `json:"legacy_endpoints"`
	LegacyAltEndpoint          string                  `json:"legacy_alt_endpoint"`
	SingleEndpoint             string                  `json:"single_endpoint"`
	SingleRegisteredEndpoint   string                  `json:"single_registered_endpoint"`
	SingleDeviceBase           string                  `json:"single_device_base"`
	SingleRegisteredDeviceBase string                  `json:"single_registered_device_base"`
	RouteHosts                 []string                `json:"route_hosts"`
	VideoTab                   miniReelProtocolProfile `json:"video_tab"`
	Single                     miniReelProtocolProfile `json:"single"`
	Registered                 miniReelProtocolProfile `json:"registered"`
	AppUA                      string                  `json:"app_ua"`
	PlaybackUA                 string                  `json:"playback_ua"`
	FallbackUA                 string                  `json:"fallback_ua"`
	SingleUA                   string                  `json:"single_ua"`
	RegisteredUA               string                  `json:"registered_ua"`
}

var miniReelRemoteConfigState = struct {
	sync.RWMutex
	path         string
	value        *miniReelRemoteConfig
	fetchStarted bool
}{}

// initMiniReelRemoteConfig loads the cached config (if any) and starts a
// background refresh. It never fails startup: every error is logged and
// ignored, and the baked-in values keep working.
func initMiniReelRemoteConfig(path string) {
	loadMiniReelRemoteConfigCache(path)
	go refreshMiniReelRemoteConfig()
}

func loadMiniReelRemoteConfigCache(path string) {
	miniReelRemoteConfigState.Lock()
	miniReelRemoteConfigState.path = path
	miniReelRemoteConfigState.Unlock()
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	cfg, err := parseMiniReelRemoteConfig(b)
	if err != nil {
		log.Printf("[minireel] remote config cache is invalid, ignoring it: %v", err)
		return
	}
	miniReelRemoteConfigState.Lock()
	miniReelRemoteConfigState.value = cfg
	miniReelRemoteConfigState.Unlock()
	log.Printf("[minireel] remote config loaded from cache")
}

// refreshMiniReelRemoteConfig fetches the config once per process with a
// short timeout. On any failure it silently keeps the current values.
func refreshMiniReelRemoteConfig() {
	miniReelRemoteConfigState.Lock()
	if miniReelRemoteConfigState.fetchStarted {
		miniReelRemoteConfigState.Unlock()
		return
	}
	miniReelRemoteConfigState.fetchStarted = true
	path := miniReelRemoteConfigState.path
	miniReelRemoteConfigState.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), miniReelRemoteConfigTimeout)
	defer cancel()
	cfg, err := fetchMiniReelRemoteConfig(ctx, miniReelRemoteConfigURL)
	if err != nil {
		log.Printf("[minireel] remote config fetch failed, keeping built-in values: %v", err)
		return
	}
	miniReelRemoteConfigState.Lock()
	miniReelRemoteConfigState.value = cfg
	miniReelRemoteConfigState.Unlock()
	if path != "" {
		if err := writeMiniReelRemoteConfigCache(path, cfg); err != nil {
			log.Printf("[minireel] remote config cache write failed: %v", err)
		}
	}
	log.Printf("[minireel] remote config updated")
}

func fetchMiniReelRemoteConfig(ctx context.Context, rawURL string) (*miniReelRemoteConfig, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("remote config HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, miniReelRemoteConfigMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > miniReelRemoteConfigMaxBytes {
		return nil, errors.New("remote config too large")
	}
	return parseMiniReelRemoteConfig(b)
}

func parseMiniReelRemoteConfig(b []byte) (*miniReelRemoteConfig, error) {
	var cfg miniReelRemoteConfig
	dec := json.NewDecoder(bytes.NewReader(b))
	if err := dec.Decode(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func writeMiniReelRemoteConfigCache(path string, cfg *miniReelRemoteConfig) error {
	b, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func miniReelRemoteConfigCurrent() *miniReelRemoteConfig {
	miniReelRemoteConfigState.RLock()
	defer miniReelRemoteConfigState.RUnlock()
	return miniReelRemoteConfigState.value
}

// Empty or invalid config values fall back to the baked-in default, field by
// field, so a partial or sloppy config can never break playback.
func miniReelRemoteString(field, def string) string {
	if strings.TrimSpace(field) == "" {
		return def
	}
	return field
}

func miniReelRemoteURL(field, def string) string {
	field = strings.TrimSpace(field)
	if field == "" {
		return def
	}
	u, err := url.Parse(field)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return def
	}
	return field
}

func miniReelRemoteURLList(fields []string, def []string) []string {
	if len(fields) == 0 {
		return def
	}
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if v := miniReelRemoteURL(f, ""); v != "" {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return def
	}
	return out
}

func miniReelRemoteHostList(fields []string, def []string) []string {
	if len(fields) == 0 {
		return def
	}
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f != "" && !strings.Contains(f, "://") && !strings.Contains(f, "/") {
			out = append(out, f)
		}
	}
	if len(out) == 0 {
		return def
	}
	return out
}

func miniReelRemoteProfile(field miniReelProtocolProfile, def miniReelProtocolProfile) miniReelProtocolProfile {
	return miniReelProtocolProfile{
		VersionCode:        miniReelRemoteString(field.VersionCode, def.VersionCode),
		VersionName:        miniReelRemoteString(field.VersionName, def.VersionName),
		Channel:            miniReelRemoteString(field.Channel, def.Channel),
		AID:                miniReelRemoteString(field.AID, def.AID),
		DeviceType:         miniReelRemoteString(field.DeviceType, def.DeviceType),
		DeviceBrand:        miniReelRemoteString(field.DeviceBrand, def.DeviceBrand),
		DeviceManufacturer: miniReelRemoteString(field.DeviceManufacturer, def.DeviceManufacturer),
		OSAPI:              miniReelRemoteString(field.OSAPI, def.OSAPI),
		OSVersion:          miniReelRemoteString(field.OSVersion, def.OSVersion),
		Resolution:         miniReelRemoteString(field.Resolution, def.Resolution),
		DPI:                miniReelRemoteString(field.DPI, def.DPI),
	}
}

// The baked-in defaults. With no config loaded every effective accessor
// returns exactly these values, so behaviour is unchanged.
func miniReelDefaultVideoTabProfile() miniReelProtocolProfile {
	return miniReelProtocolProfile{
		VersionCode: "71332", VersionName: "7.1.3.32", Channel: "update_64", AID: "8662",
		DeviceType: "25053RT47C", DeviceBrand: "Redmi", DeviceManufacturer: "Redmi",
		OSAPI: "36", OSVersion: "16", Resolution: "1280*2772", DPI: "520",
	}
}

func miniReelDefaultSingleProfile() miniReelProtocolProfile {
	return miniReelProtocolProfile{
		VersionCode: "73932", VersionName: "7.3.9.32", Channel: "xiaomi_8662_64", AID: "8662",
		DeviceType: "23127PN0CC", DeviceBrand: "Xiaomi", DeviceManufacturer: "Xiaomi",
		OSAPI: "34", OSVersion: "14", Resolution: "1200*2670", DPI: "460",
	}
}

func miniReelDefaultRegisteredProfile() miniReelProtocolProfile {
	return miniReelProtocolProfile{
		VersionCode: "73532", VersionName: "7.3.5.32", Channel: "update_64", AID: "8662",
		DeviceType: "25053RT47C", DeviceBrand: "Redmi", DeviceManufacturer: "Xiaomi",
		OSAPI: "36", OSVersion: "16", Resolution: "1280*2772", DPI: "520",
	}
}

func miniReelDefaultLegacyEndpoints() []string {
	return []string{
		"https://api5-normal-sinfonlineb.fqnovel.com" + miniReelAPIPath,
		"https://api5-normal-hl.fqnovel.com" + miniReelAPIPath,
	}
}

func miniReelDefaultRouteHosts() []string {
	return []string{
		"api5-normal-sinfonlinea.fqnovel.com",
		"api5-normal-sinfonlinec.fqnovel.com",
		"api5-normal-sinfonlineb.fqnovel.com",
		"api3-normal-sinfonlinea.fqnovel.com",
		"api3-normal-sinfonlineb.fqnovel.com",
		"api3-normal-sinfonlinec.fqnovel.com",
	}
}

// Effective values: remote config wins per field, baked-in defaults otherwise.
func miniReelEffectiveVideoTabEndpoint() string {
	if cfg := miniReelRemoteConfigCurrent(); cfg != nil {
		return miniReelRemoteURL(cfg.VideoTabEndpoint, miniReelVideoTabBase+miniReelVideoTabPath)
	}
	return miniReelVideoTabBase + miniReelVideoTabPath
}

func miniReelEffectiveLegacyAPIEndpoint() string {
	if cfg := miniReelRemoteConfigCurrent(); cfg != nil {
		return miniReelRemoteURL(cfg.LegacyAPIEndpoint, miniReelAPIBase+miniReelAPIPath)
	}
	return miniReelAPIBase + miniReelAPIPath
}

func miniReelEffectiveLegacyAPIPath() string {
	endpoint := miniReelEffectiveLegacyAPIEndpoint()
	if u, err := url.Parse(endpoint); err == nil && u.Path != "" {
		return u.Path
	}
	return miniReelAPIPath
}

func miniReelEffectiveLegacyEndpoints() []string {
	if cfg := miniReelRemoteConfigCurrent(); cfg != nil {
		return miniReelRemoteURLList(cfg.LegacyEndpoints, miniReelDefaultLegacyEndpoints())
	}
	return miniReelDefaultLegacyEndpoints()
}

func miniReelEffectiveLegacyAltEndpoint() string {
	if cfg := miniReelRemoteConfigCurrent(); cfg != nil {
		if v := miniReelRemoteURL(cfg.LegacyAltEndpoint, ""); v != "" {
			return v
		}
		if endpoints := miniReelRemoteURLList(cfg.LegacyEndpoints, nil); len(endpoints) > 1 {
			return endpoints[1]
		}
	}
	return "https://api5-normal-hl.fqnovel.com" + miniReelAPIPath
}

func miniReelEffectiveSingleEndpoint() string {
	if cfg := miniReelRemoteConfigCurrent(); cfg != nil {
		return miniReelRemoteURL(cfg.SingleEndpoint, miniReelSingleEndpoint)
	}
	return miniReelSingleEndpoint
}

func miniReelEffectiveSingleRegisteredEndpoint() string {
	if cfg := miniReelRemoteConfigCurrent(); cfg != nil {
		return miniReelRemoteURL(cfg.SingleRegisteredEndpoint, singleRegisteredEndpoint)
	}
	return singleRegisteredEndpoint
}

func miniReelEffectiveSingleDeviceBase() string {
	if cfg := miniReelRemoteConfigCurrent(); cfg != nil {
		return miniReelRemoteURL(cfg.SingleDeviceBase, miniReelSingleDeviceBase)
	}
	return miniReelSingleDeviceBase
}

func miniReelEffectiveSingleRegisteredDeviceBase() string {
	if cfg := miniReelRemoteConfigCurrent(); cfg != nil {
		return miniReelRemoteURL(cfg.SingleRegisteredDeviceBase, singleRegisteredDeviceBase)
	}
	return singleRegisteredDeviceBase
}

func miniReelEffectiveRouteHosts() []string {
	if cfg := miniReelRemoteConfigCurrent(); cfg != nil {
		return miniReelRemoteHostList(cfg.RouteHosts, miniReelDefaultRouteHosts())
	}
	return miniReelDefaultRouteHosts()
}

func miniReelEffectiveVideoTabProfile() miniReelProtocolProfile {
	if cfg := miniReelRemoteConfigCurrent(); cfg != nil {
		return miniReelRemoteProfile(cfg.VideoTab, miniReelDefaultVideoTabProfile())
	}
	return miniReelDefaultVideoTabProfile()
}

func miniReelEffectiveSingleProfile() miniReelProtocolProfile {
	if cfg := miniReelRemoteConfigCurrent(); cfg != nil {
		return miniReelRemoteProfile(cfg.Single, miniReelDefaultSingleProfile())
	}
	return miniReelDefaultSingleProfile()
}

func miniReelEffectiveRegisteredProfile() miniReelProtocolProfile {
	if cfg := miniReelRemoteConfigCurrent(); cfg != nil {
		return miniReelRemoteProfile(cfg.Registered, miniReelDefaultRegisteredProfile())
	}
	return miniReelDefaultRegisteredProfile()
}

func miniReelEffectiveAppUA() string {
	if cfg := miniReelRemoteConfigCurrent(); cfg != nil {
		return miniReelRemoteString(cfg.AppUA, miniReelAppUA)
	}
	return miniReelAppUA
}

func miniReelEffectivePlaybackUA() string {
	if cfg := miniReelRemoteConfigCurrent(); cfg != nil {
		return miniReelRemoteString(cfg.PlaybackUA, miniReelPlaybackUA)
	}
	return miniReelPlaybackUA
}

func miniReelEffectiveFallbackUA() string {
	if cfg := miniReelRemoteConfigCurrent(); cfg != nil {
		return miniReelRemoteString(cfg.FallbackUA, miniReelFallbackUA)
	}
	return miniReelFallbackUA
}

func miniReelEffectiveSingleUA() string {
	if cfg := miniReelRemoteConfigCurrent(); cfg != nil {
		return miniReelRemoteString(cfg.SingleUA, miniReelSingleUA)
	}
	return miniReelSingleUA
}

func miniReelEffectiveRegisteredUA() string {
	if cfg := miniReelRemoteConfigCurrent(); cfg != nil {
		return miniReelRemoteString(cfg.RegisteredUA, singleRegisteredUA)
	}
	return singleRegisteredUA
}

// miniReelPayloadResolution converts a query resolution like "1200*2670" to
// the device_register payload form "2670x1200". Anything else passes through.
func miniReelPayloadResolution(resolution string) string {
	parts := strings.Split(resolution, "*")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return resolution
	}
	return parts[1] + "x" + parts[0]
}

// mustMiniReelProfileInt parses a numeric profile field for the
// device_register payload. Config values are maintainer-edited; a bad value
// degrades to 0 for that one field rather than breaking the request.
func mustMiniReelProfileInt(value string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(value))
	return n
}
