package main

// The single-episode protocol is deliberately isolated from account requests,
// video_tab's reusable identity and the legacy POST's profile.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const miniReelSinglePath = "/novel/player/video_model/v1/"
const miniReelSingleEndpoint = "https://api5-normal-sinfonlinea.fqnovel.com" + miniReelSinglePath
const miniReelSingleDeviceBase = "https://log5-applog-lq.fqnovel.com"
const miniReelSingleUA = "com.phoenix.read/73932 (Linux; U; Android 14; zh_CN; 23127PN0CC; Build/UKQ1.230804.001; Cronet/TTNetVersion:8d40f833 2026-03-03 QuicVersion:21ac1950 2025-11-18)"

func miniReelSingleDeviceQuery(d miniReelDevice) url.Values {
	// Version/channel/device strings follow the remote config so a new
	// official protocol can be adopted without an app release.
	profile := miniReelEffectiveSingleProfile()
	return url.Values{
		"iid": {d.installID}, "device_id": {d.deviceID}, "ac": {"wifi"}, "channel": {profile.Channel},
		"aid": {profile.AID}, "app_name": {"novelread"}, "cdid": {d.cdid}, "version_code": {profile.VersionCode}, "version_name": {profile.VersionName},
		"device_platform": {"android"}, "os": {"android"}, "ssmix": {"a"}, "device_type": {profile.DeviceType}, "device_brand": {profile.DeviceBrand},
		"language": {"zh"}, "os_api": {profile.OSAPI}, "os_version": {profile.OSVersion}, "manifest_version_code": {profile.VersionCode},
		"resolution": {profile.Resolution}, "dpi": {profile.DPI}, "update_version_code": {profile.VersionCode}, "host_abi": {"arm64-v8a"},
		"dragon_device_type": {"phone"}, "pv_player": {profile.VersionCode}, "compliance_status": {"0"}, "need_personal_recommend": {"1"},
		"player_so_load": {"1"}, "is_android_pad_screen": {"0"}, "openudid": {d.openUDID},
	}
}

func miniReelSingleDevicePayload(d *passportDevice, stamp int64) []byte {
	var p map[string]any
	_ = json.Unmarshal(d.payload(stamp), &p)
	h := p["header"].(map[string]any)
	profile := miniReelEffectiveSingleProfile()
	versionCode, _ := strconv.Atoi(profile.VersionCode)
	for k, v := range map[string]any{"display_name": "红果免费短剧", "app_version": profile.VersionName, "app_version_minor": profile.VersionName, "version_code": versionCode, "manifest_version_code": versionCode, "update_version_code": versionCode, "channel": profile.Channel, "device_brand": profile.DeviceBrand, "device_manufacturer": profile.DeviceManufacturer, "device_model": profile.DeviceType, "os_api": mustMiniReelProfileInt(profile.OSAPI), "os_version": profile.OSVersion, "density_dpi": mustMiniReelProfileInt(profile.DPI), "resolution": miniReelPayloadResolution(profile.Resolution), "device_id": "0", "install_id": "0", "req_id": d.ClientUDID} {
		h[k] = v
	}
	h["oaid"] = map[string]any{"req_id": d.CDID, "hw_id_version_code": "0", "take_ms": "6", "is_track_limited": "false", "query_times": "1", "id": d.CDID, "time": strconv.FormatInt(stamp-200, 10)}
	b, _ := json.Marshal(p)
	return b
}

func registerMiniReelSingleDevice(ctx context.Context) (miniReelDevice, error) {
	return registerMiniReelSingleDeviceAt(ctx, miniReelEffectiveSingleDeviceBase(), nil)
}
func registerMiniReelSingleDeviceAt(ctx context.Context, base string, httpClient *http.Client) (miniReelDevice, error) {
	if err := ctx.Err(); err != nil {
		return miniReelDevice{}, err
	}
	d, err := newPassportDevice()
	if err != nil {
		return miniReelDevice{}, err
	}
	c := newPassportClient(accountSession{Device: d, Cookies: map[string]string{}})
	c.deviceBase = base
	if httpClient != nil {
		c.http = httpClient
	}
	ctx, cancel := context.WithTimeout(ctx, miniReelDeviceTimeout)
	defer cancel()
	if err = c.registerDeviceProfile(ctx, true); err != nil {
		return miniReelDevice{}, err
	}
	return miniReelDevice{deviceID: c.session.DeviceID, installID: c.session.InstallID, cdid: d.CDID, openUDID: d.OpenUDID, cookie: miniReelPlaybackCookieHeader(c.session.Cookies)}, nil
}

func miniReelSingleOptions(ctx context.Context, vid, endpoint, codecMode string, device miniReelDevice, client *http.Client) (options []miniReelOption, err error) {
	started := time.Now()
	diagnostic := playbackPreparationDiagnostic{Code: "VIDEO_MODEL_SIGN", Stage: "video_model"}
	defer func() { err = finishPlaybackPreparation(err, diagnostic, started) }()
	body, _ := json.Marshal(map[string]any{"video_id": vid, "biz_param": map[string]any{"video_id_type": 0, "need_all_video_definition": true, "video_platform": 3}})
	stamp := time.Now().UnixMilli()
	q := miniReelSingleDeviceQuery(device)
	query, headers, err := miniReelSignPlaybackRequest(q, body, stamp, device.deviceID)
	if err != nil {
		return nil, errors.New("single playback signing failed")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"?"+query, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header = headers
	request.Header.Set("User-Agent", miniReelEffectiveSingleUA())
	request.Header.Set("Content-Type", "application/json; charset=UTF-8")
	request.Header.Set("Accept", "application/json; charset=utf-8,application/x-protobuf")
	request.Header.Set("Passport-Sdk-Version", "50561")
	request.Header.Set("X-Vc-Bdturing-Sdk-Version", "3.7.2.cn")
	request.Header.Set("X-Tt-Request-Tag", "t=0;n=0")
	request.Header.Set("X-SS-DP", "8662")
	request.Header.Set("Lc", "101")
	request.Header.Set("Cookie", device.cookie)
	nonce, _ := protocolRandom()
	request.Header.Set("X-Reading-Request", fmt.Sprintf("%d-%d", stamp, nonce&0x7fffffff))
	diagnostic.Code = "VIDEO_MODEL_TRANSPORT"
	response, err := miniReelNoRedirectClient(client, 8*time.Second).Do(request)
	if err != nil {
		var e *url.Error
		if errors.As(err, &e) {
			return nil, e.Err
		}
		return nil, err
	}
	defer response.Body.Close()
	diagnostic.HTTPStatus = &response.StatusCode
	diagnostic.Code = "VIDEO_MODEL_HTTP"
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("single playback HTTP %d", response.StatusCode)
	}
	diagnostic.Code = "VIDEO_MODEL_READ"
	data, err := miniReelReadLimited(response.Body, miniReelMaxJSONBytes)
	if err != nil {
		return nil, err
	}
	n := len(data)
	diagnostic.ResponseBytes = &n
	diagnostic.Code = "VIDEO_MODEL_EMPTY"
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errMiniReelVideoModelEmpty
	}
	diagnostic.Code = "VIDEO_MODEL_JSON"
	var envelope map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if decoder.Decode(&envelope) != nil || envelope == nil {
		return nil, errors.New("single playback response is invalid JSON")
	}
	return miniReelOptionsFromEnvelope(ctx, vid, envelope, client, codecMode, &diagnostic)
}
