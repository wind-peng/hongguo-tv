package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// setMiniReelRemoteConfigForTest swaps the active remote config and restores
// it when the test ends. Tests in this package run sequentially.
func setMiniReelRemoteConfigForTest(t *testing.T, cfg *miniReelRemoteConfig) {
	t.Helper()
	miniReelRemoteConfigState.Lock()
	prevValue := miniReelRemoteConfigState.value
	prevPath := miniReelRemoteConfigState.path
	miniReelRemoteConfigState.value = cfg
	miniReelRemoteConfigState.Unlock()
	t.Cleanup(func() {
		miniReelRemoteConfigState.Lock()
		miniReelRemoteConfigState.value = prevValue
		miniReelRemoteConfigState.path = prevPath
		miniReelRemoteConfigState.Unlock()
	})
}

func TestMiniReelRemoteConfigDefaults(t *testing.T) {
	setMiniReelRemoteConfigForTest(t, nil)
	if got := miniReelEffectiveVideoTabEndpoint(); got != miniReelVideoTabBase+miniReelVideoTabPath {
		t.Errorf("video tab endpoint = %q", got)
	}
	if got := miniReelEffectiveLegacyAPIEndpoint(); got != miniReelAPIBase+miniReelAPIPath {
		t.Errorf("legacy api endpoint = %q", got)
	}
	if got := miniReelEffectiveLegacyAPIPath(); got != miniReelAPIPath {
		t.Errorf("legacy api path = %q", got)
	}
	if got := miniReelEffectiveLegacyEndpoints(); !reflect.DeepEqual(got, miniReelDefaultLegacyEndpoints()) {
		t.Errorf("legacy endpoints = %v", got)
	}
	if got := miniReelEffectiveLegacyAltEndpoint(); got != "https://api5-normal-hl.fqnovel.com"+miniReelAPIPath {
		t.Errorf("legacy alt endpoint = %q", got)
	}
	if got := miniReelEffectiveSingleEndpoint(); got != miniReelSingleEndpoint {
		t.Errorf("single endpoint = %q", got)
	}
	if got := miniReelEffectiveSingleRegisteredEndpoint(); got != singleRegisteredEndpoint {
		t.Errorf("single registered endpoint = %q", got)
	}
	if got := miniReelEffectiveSingleDeviceBase(); got != miniReelSingleDeviceBase {
		t.Errorf("single device base = %q", got)
	}
	if got := miniReelEffectiveSingleRegisteredDeviceBase(); got != singleRegisteredDeviceBase {
		t.Errorf("single registered device base = %q", got)
	}
	if got := miniReelEffectiveRouteHosts(); !reflect.DeepEqual(got, miniReelDefaultRouteHosts()) {
		t.Errorf("route hosts = %v", got)
	}
	if got := miniReelEffectiveVideoTabProfile(); !reflect.DeepEqual(got, miniReelDefaultVideoTabProfile()) {
		t.Errorf("video tab profile = %+v", got)
	}
	if got := miniReelEffectiveSingleProfile(); !reflect.DeepEqual(got, miniReelDefaultSingleProfile()) {
		t.Errorf("single profile = %+v", got)
	}
	if got := miniReelEffectiveRegisteredProfile(); !reflect.DeepEqual(got, miniReelDefaultRegisteredProfile()) {
		t.Errorf("registered profile = %+v", got)
	}
	if got := miniReelEffectiveAppUA(); got != miniReelAppUA {
		t.Errorf("app UA = %q", got)
	}
	if got := miniReelEffectivePlaybackUA(); got != miniReelPlaybackUA {
		t.Errorf("playback UA = %q", got)
	}
	if got := miniReelEffectiveFallbackUA(); got != miniReelFallbackUA {
		t.Errorf("fallback UA = %q", got)
	}
	if got := miniReelEffectiveSingleUA(); got != miniReelSingleUA {
		t.Errorf("single UA = %q", got)
	}
	if got := miniReelEffectiveRegisteredUA(); got != singleRegisteredUA {
		t.Errorf("registered UA = %q", got)
	}
}

func TestMiniReelRemoteConfigOverrides(t *testing.T) {
	setMiniReelRemoteConfigForTest(t, &miniReelRemoteConfig{
		VideoTabEndpoint: "https://example.invalid/tab/v2/",
		SingleEndpoint:   "https://example.invalid/single/v2/",
		LegacyEndpoints:  []string{"https://example.invalid/a/", "https://example.invalid/b/"},
		Single:           miniReelProtocolProfile{VersionName: "9.9.9.99", DeviceType: "TESTMODEL"},
		RegisteredUA:     "test-ua/1.0",
	})
	if got := miniReelEffectiveVideoTabEndpoint(); got != "https://example.invalid/tab/v2/" {
		t.Errorf("video tab endpoint = %q", got)
	}
	if got := miniReelEffectiveSingleEndpoint(); got != "https://example.invalid/single/v2/" {
		t.Errorf("single endpoint = %q", got)
	}
	if got := miniReelEffectiveLegacyEndpoints(); !reflect.DeepEqual(got, []string{"https://example.invalid/a/", "https://example.invalid/b/"}) {
		t.Errorf("legacy endpoints = %v", got)
	}
	if got := miniReelEffectiveLegacyAltEndpoint(); got != "https://example.invalid/b/" {
		t.Errorf("legacy alt endpoint = %q", got)
	}
	// Partial override: set fields fall back to the baked-in values.
	if got := miniReelEffectiveSingleProfile(); got.VersionName != "9.9.9.99" || got.DeviceType != "TESTMODEL" || got.VersionCode != "73932" || got.Channel != "xiaomi_8662_64" {
		t.Errorf("single profile = %+v", got)
	}
	if got := miniReelEffectiveRegisteredUA(); got != "test-ua/1.0" {
		t.Errorf("registered UA = %q", got)
	}
	if got := miniReelEffectiveSingleRegisteredEndpoint(); got != singleRegisteredEndpoint {
		t.Errorf("unconfigured endpoint changed: %q", got)
	}
}

func TestMiniReelRemoteConfigIgnoresInvalidValues(t *testing.T) {
	setMiniReelRemoteConfigForTest(t, &miniReelRemoteConfig{
		VideoTabEndpoint: "not a url",
		SingleEndpoint:   "ftp://example.invalid/x",
		LegacyEndpoints:  []string{"::bad", ""},
		RouteHosts:       []string{"has/slash", "https://example.invalid", ""},
	})
	if got := miniReelEffectiveVideoTabEndpoint(); got != miniReelVideoTabBase+miniReelVideoTabPath {
		t.Errorf("invalid video tab endpoint was used: %q", got)
	}
	if got := miniReelEffectiveSingleEndpoint(); got != miniReelSingleEndpoint {
		t.Errorf("invalid single endpoint was used: %q", got)
	}
	if got := miniReelEffectiveLegacyEndpoints(); !reflect.DeepEqual(got, miniReelDefaultLegacyEndpoints()) {
		t.Errorf("invalid legacy endpoints were used: %v", got)
	}
	if got := miniReelEffectiveRouteHosts(); !reflect.DeepEqual(got, miniReelDefaultRouteHosts()) {
		t.Errorf("invalid route hosts were used: %v", got)
	}
}

func TestMiniReelRemoteConfigFetch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"single_endpoint":"https://example.invalid/fetched/","single":{"version_name":"8.0.0.1"}}`))
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cfg, err := fetchMiniReelRemoteConfig(ctx, server.URL)
	if err != nil {
		t.Fatalf("fetch failed: %v", err)
	}
	setMiniReelRemoteConfigForTest(t, cfg)
	if got := miniReelEffectiveSingleEndpoint(); got != "https://example.invalid/fetched/" {
		t.Errorf("fetched single endpoint = %q", got)
	}
	if got := miniReelEffectiveSingleProfile(); got.VersionName != "8.0.0.1" || got.VersionCode != "73932" {
		t.Errorf("fetched single profile = %+v", got)
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer bad.Close()
	if _, err := fetchMiniReelRemoteConfig(ctx, bad.URL); err == nil {
		t.Error("HTTP 500 fetch should fail")
	}
	invalid := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{not json`))
	}))
	defer invalid.Close()
	if _, err := fetchMiniReelRemoteConfig(ctx, invalid.URL); err == nil {
		t.Error("invalid JSON fetch should fail")
	}
}

func TestMiniReelRemoteConfigCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "minireel-remote-config.json")
	want := &miniReelRemoteConfig{
		VideoTabEndpoint: "https://example.invalid/cached/",
		Single:           miniReelProtocolProfile{VersionName: "8.1.0.1"},
	}
	if err := writeMiniReelRemoteConfigCache(path, want); err != nil {
		t.Fatalf("cache write failed: %v", err)
	}
	setMiniReelRemoteConfigForTest(t, nil)
	loadMiniReelRemoteConfigCache(path)
	if got := miniReelEffectiveVideoTabEndpoint(); got != "https://example.invalid/cached/" {
		t.Errorf("cached video tab endpoint = %q", got)
	}
	if got := miniReelEffectiveSingleProfile(); got.VersionName != "8.1.0.1" {
		t.Errorf("cached single profile = %+v", got)
	}
	// A corrupt cache file must be ignored, not crash startup.
	if err := os.WriteFile(path, []byte(`{broken`), 0600); err != nil {
		t.Fatalf("corrupt cache write failed: %v", err)
	}
	setMiniReelRemoteConfigForTest(t, nil)
	loadMiniReelRemoteConfigCache(path)
	if got := miniReelEffectiveVideoTabEndpoint(); got != miniReelVideoTabBase+miniReelVideoTabPath {
		t.Errorf("corrupt cache changed endpoint: %q", got)
	}
}

// The shipped remote-config.json must describe exactly the baked-in values,
// so installing it changes nothing until a maintainer edits it.
func TestMiniReelRemoteConfigShippedFileMatchesBuiltins(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "remote-config.json"))
	if err != nil {
		t.Fatalf("read remote-config.json: %v", err)
	}
	cfg, err := parseMiniReelRemoteConfig(b)
	if err != nil {
		t.Fatalf("parse remote-config.json: %v", err)
	}
	setMiniReelRemoteConfigForTest(t, cfg)
	if got := miniReelEffectiveVideoTabEndpoint(); got != miniReelVideoTabBase+miniReelVideoTabPath {
		t.Errorf("shipped video tab endpoint = %q", got)
	}
	if got := miniReelEffectiveLegacyAPIEndpoint(); got != miniReelAPIBase+miniReelAPIPath {
		t.Errorf("shipped legacy api endpoint = %q", got)
	}
	if got := miniReelEffectiveLegacyEndpoints(); !reflect.DeepEqual(got, miniReelDefaultLegacyEndpoints()) {
		t.Errorf("shipped legacy endpoints = %v", got)
	}
	if got := miniReelEffectiveSingleEndpoint(); got != miniReelSingleEndpoint {
		t.Errorf("shipped single endpoint = %q", got)
	}
	if got := miniReelEffectiveSingleRegisteredEndpoint(); got != singleRegisteredEndpoint {
		t.Errorf("shipped single registered endpoint = %q", got)
	}
	if got := miniReelEffectiveSingleDeviceBase(); got != miniReelSingleDeviceBase {
		t.Errorf("shipped single device base = %q", got)
	}
	if got := miniReelEffectiveSingleRegisteredDeviceBase(); got != singleRegisteredDeviceBase {
		t.Errorf("shipped single registered device base = %q", got)
	}
	if got := miniReelEffectiveRouteHosts(); !reflect.DeepEqual(got, miniReelDefaultRouteHosts()) {
		t.Errorf("shipped route hosts = %v", got)
	}
	if got := miniReelEffectiveVideoTabProfile(); !reflect.DeepEqual(got, miniReelDefaultVideoTabProfile()) {
		t.Errorf("shipped video tab profile = %+v", got)
	}
	if got := miniReelEffectiveSingleProfile(); !reflect.DeepEqual(got, miniReelDefaultSingleProfile()) {
		t.Errorf("shipped single profile = %+v", got)
	}
	if got := miniReelEffectiveRegisteredProfile(); !reflect.DeepEqual(got, miniReelDefaultRegisteredProfile()) {
		t.Errorf("shipped registered profile = %+v", got)
	}
	if got := miniReelEffectiveAppUA(); got != miniReelAppUA {
		t.Errorf("shipped app UA = %q", got)
	}
	if got := miniReelEffectivePlaybackUA(); got != miniReelPlaybackUA {
		t.Errorf("shipped playback UA = %q", got)
	}
	if got := miniReelEffectiveFallbackUA(); got != miniReelFallbackUA {
		t.Errorf("shipped fallback UA = %q", got)
	}
	if got := miniReelEffectiveSingleUA(); got != miniReelSingleUA {
		t.Errorf("shipped single UA = %q", got)
	}
	if got := miniReelEffectiveRegisteredUA(); got != singleRegisteredUA {
		t.Errorf("shipped registered UA = %q", got)
	}
}

func TestMiniReelRemoteConfigProductionSources(t *testing.T) {
	setMiniReelRemoteConfigForTest(t, &miniReelRemoteConfig{
		VideoTabEndpoint:         "https://example.invalid/tab/v3/",
		SingleEndpoint:           "https://example.invalid/single/v3/",
		SingleRegisteredEndpoint: "https://example.invalid/reg/v3/",
		LegacyEndpoints:          []string{"https://example.invalid/l1/", "https://example.invalid/l2/"},
	})
	sources := miniReelProductionSources()
	if sources.tabEndpoint != "https://example.invalid/tab/v3/" {
		t.Errorf("tab endpoint = %q", sources.tabEndpoint)
	}
	if sources.singleEndpoint != "https://example.invalid/single/v3/" {
		t.Errorf("single endpoint = %q", sources.singleEndpoint)
	}
	if sources.registeredSingle == nil || sources.registeredSingle.endpoint != "https://example.invalid/reg/v3/" {
		t.Errorf("registered single endpoint = %v", sources.registeredSingle)
	}
	if !reflect.DeepEqual(sources.legacyEndpoints, []string{"https://example.invalid/l1/", "https://example.invalid/l2/"}) {
		t.Errorf("legacy endpoints = %v", sources.legacyEndpoints)
	}
}

func TestMiniReelPayloadResolution(t *testing.T) {
	if got := miniReelPayloadResolution("1200*2670"); got != "2670x1200" {
		t.Errorf("got %q", got)
	}
	if got := miniReelPayloadResolution("1280*2772"); got != "2772x1280" {
		t.Errorf("got %q", got)
	}
	if got := miniReelPayloadResolution("2670x1200"); got != "2670x1200" {
		t.Errorf("got %q", got)
	}
	if got := miniReelPayloadResolution(""); got != "" {
		t.Errorf("got %q", got)
	}
}
