// DockerSpeedTest —— 免 Docker 的镜像加速源测速工具
// 直接用 Docker Registry HTTP API v2 拉取镜像清单并下载层，测量真实拉取速度。
package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	appName    = "DockerSpeedTest"
	appVersion = "1.1.1"
	acceptHdr  = "application/vnd.docker.distribution.manifest.list.v2+json, " +
		"application/vnd.docker.distribution.manifest.v2+json, " +
		"application/vnd.oci.image.index.v1+json, " +
		"application/vnd.oci.image.manifest.v1+json, " +
		"application/vnd.docker.distribution.manifest.v1+prettyjws"
	uaHdr = "docker/27.3.1 go/1.22.7 kernel/10.0.19045 os/windows arch/amd64 UpstreamClient(Docker-Client/27.3.1)"
)

// ---------------- 配置 ----------------

type Config struct {
	Image           string
	ConsoleEnc      string // utf8 / gbk / auto
	Timeout         time.Duration // 单次请求(清单/认证)超时
	DownloadTimeout time.Duration // 单层下载超时
	Phase1          int           // 阶段1并发
	Speed           int           // 阶段2并发(1=顺序,速度最准)
	MinTestMB       float64       // >0 时反复下载使总量达到该值,速度更稳
	SaveCSV         bool
	Pause           bool
	Proxy           string
}

func defaultConfig() Config {
	return Config{
		Image:           "busybox:latest",
		ConsoleEnc:      "utf8",
		Timeout:         20 * time.Second,
		DownloadTimeout: 120 * time.Second,
		Phase1:          12,
		Speed:           1,
		MinTestMB:       0,
		SaveCSV:         true,
		Pause:           true,
	}
}

const defaultConfigText = `# DockerSpeedTest 配置文件（改完保存，重新运行 exe 生效）
image=busybox:latest        # 测试镜像，可换成 nginx:latest / alpine 等
console_encoding=utf8       # 控制台编码：utf8(推荐,Win10+全终端不乱码) / gbk(老终端) / auto(跟随原代码页)
timeout=20                  # 单个请求超时（秒）
download_timeout=120        # 单层下载超时（秒）
phase1_concurrency=12       # 阶段1(连通性检测)并发数
speed_concurrency=1         # 阶段2(测速)并发数，1=顺序拉取，速度互不抢带宽
min_test_mb=0               # >0 时反复下载凑够该流量，速度更稳定，如 10
save_csv=true               # 结果另存为 CSV（UTF-8，Excel 可直接打开）
pause=true                  # 双击运行结束后等待回车，防止窗口闪退
proxy=                      # 可选代理，如 http://127.0.0.1:10808，留空直连
`

const defaultMirrorsText = `# Docker 镜像加速测试列表（每行一个地址，支持 # 注释）
# 兼容粘贴格式：@url:` + "`" + `https://xxx` + "`" + ` 也会被识别
https://docker.1ms.run
https://docker.xuanyuan.me
https://docker.m.daocloud.io
https://docker.1panel.live
https://dockerproxy.net
https://dockerproxy.link
https://proxy.vvvv.ee
https://docker.jiaxin.site
https://registry.cyou
https://mirror.houlang.cloud
https://xuanyuan.cloud
https://atomhub.openatom.cn
https://hub.uuuadc.top
https://docker.hpcloud.cloud
https://docker-0.unsee.tech
https://public.ecr.aws
https://cr.yandex
https://hubfast.cn
https://free.hubfast.cn
`

func stripComment(s string) string {
	if i := strings.Index(s, " #"); i >= 0 {
		s = s[:i]
	}
	return s
}

func parseBool(v string, def bool) bool {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "true", "yes", "on", "1":
		return true
	case "false", "no", "off", "0":
		return false
	}
	return def
}

func loadConfig(path string) (Config, error) {
	cfg := defaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = stripComment(line)
		i := strings.Index(line, "=")
		if i < 0 {
			continue
		}
		k := strings.ToLower(strings.TrimSpace(line[:i]))
		v := strings.TrimSpace(line[i+1:])
		switch k {
		case "image":
			if v != "" {
				cfg.Image = v
			}
		case "console_encoding":
			v = strings.ToLower(v)
			if v == "utf8" || v == "gbk" || v == "auto" {
				cfg.ConsoleEnc = v
			}
		case "timeout":
			if n := atoi(v); n > 0 {
				cfg.Timeout = time.Duration(n) * time.Second
			}
		case "download_timeout":
			if n := atoi(v); n > 0 {
				cfg.DownloadTimeout = time.Duration(n) * time.Second
			}
		case "phase1_concurrency":
			if n := atoi(v); n > 0 {
				cfg.Phase1 = n
			}
		case "speed_concurrency":
			if n := atoi(v); n > 0 {
				cfg.Speed = n
			}
		case "min_test_mb":
			if f, err := atof(v); err == nil && f >= 0 {
				cfg.MinTestMB = f
			}
		case "save_csv":
			cfg.SaveCSV = parseBool(v, cfg.SaveCSV)
		case "pause":
			cfg.Pause = parseBool(v, cfg.Pause)
		case "proxy":
			cfg.Proxy = v
		}
	}
	return cfg, nil
}

func atoi(s string) int {
	var n int
	for _, r := range strings.TrimSpace(s) {
		if r < '0' || r > '9' {
			return n
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func atof(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("empty")
	}
	parts := strings.SplitN(s, ".", 2)
	whole := 0
	for _, r := range parts[0] {
		if r < '0' || r > '9' {
			return 0, errors.New("bad float")
		}
		whole = whole*10 + int(r-'0')
	}
	frac, div := 0.0, 1.0
	if len(parts) == 2 {
		for _, r := range parts[1] {
			if r < '0' || r > '9' {
				return 0, errors.New("bad float")
			}
			div /= 10
			frac += float64(r-'0') * div
		}
	}
	return float64(whole) + frac, nil
}

// ---------------- 镜像列表 ----------------

func parseMirrorLine(line string) string {
	s := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line), "\r"))
	s = strings.TrimPrefix(s, string(rune(0xFEFF))) // 首行可能带 UTF-8 BOM
	if s == "" || strings.HasPrefix(s, "#") {
		return ""
	}
	s = stripComment(s)
	s = strings.TrimSpace(s)
	low := strings.ToLower(s)
	if strings.HasPrefix(low, "@url:") {
		s = s[len("@url:"):]
	} else if strings.HasPrefix(low, "url:") {
		s = s[len("url:"):]
	}
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "`'\"")
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, ",")
	s = strings.Trim(s, "`'\"")
	s = strings.TrimSpace(s)
	if s == "" || strings.ContainsAny(s, " 	@") {
		return ""
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	return strings.TrimSuffix(u.String(), "/")
}

func loadMirrors(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []string
	seen := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		m := parseMirrorLine(line)
		if m == "" || seen[m] {
			continue
		}
		seen[m] = true
		out = append(out, m)
	}
	return out, nil
}

// ---------------- Registry 协议 ----------------

type layerInfo struct {
	digest string
	size   int64
}

type pullInfo struct {
	repo      string
	token     string
	layers    []layerInfo
	schema1   bool
	mediaType string
}

type challenge struct {
	realm, service, scope string
	basic                 bool
}

var reChallenge = regexp.MustCompile(`([a-zA-Z_]+)="([^"]*)"`)

func parseChallenge(h string) challenge {
	var c challenge
	h = strings.TrimSpace(h)
	if h == "" {
		return c
	}
	if idx := strings.Index(h, " "); idx > 0 {
		scheme := strings.ToLower(h[:idx])
		if scheme == "basic" {
			c.basic = true
		}
	}
	for _, m := range reChallenge.FindAllStringSubmatch(h, -1) {
		switch strings.ToLower(m[1]) {
		case "realm":
			c.realm = m[2]
		case "service":
			c.service = m[2]
		case "scope":
			c.scope = m[2]
		}
	}
	return c
}

func getToken(client *http.Client, ch challenge, repo string) (string, error) {
	if ch.realm == "" {
		return "", errors.New("401 但未给出认证地址")
	}
	q := url.Values{}
	if ch.service != "" {
		q.Set("service", ch.service)
	}
	scope := ch.scope
	if scope == "" {
		scope = "repository:" + repo + ":pull"
	}
	q.Set("scope", scope)
	u := ch.realm
	if strings.Contains(u, "?") {
		u += "&" + q.Encode()
	} else {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", uaHdr)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("获取token失败 HTTP %d", resp.StatusCode)
	}
	var body struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return "", errors.New("token响应格式错误")
	}
	if body.Token != "" {
		return body.Token, nil
	}
	if body.AccessToken != "" {
		return body.AccessToken, nil
	}
	return "", errors.New("token响应为空")
}

type apiError struct {
	Errors []struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
}

func apiErrMsg(b []byte) string {
	var e apiError
	if json.Unmarshal(b, &e) == nil && len(e.Errors) > 0 {
		msg := e.Errors[0].Message
		if msg == "" {
			msg = e.Errors[0].Code
		}
		if len(msg) > 60 {
			msg = msg[:60]
		}
		return msg
	}
	return ""
}

// doReq 发一个带超时的 GET；调用方读完 body 后必须调用 cancel
func doReq(ctx context.Context, client *http.Client, u, token, accept string) (*http.Response, context.CancelFunc, error) {
	reqCtx, cancel := context.WithTimeout(ctx, reqTimeout)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, u, nil)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", uaHdr)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	return resp, cancel, nil
}

// readBody 读响应体（限流）
func readBody(resp *http.Response, limit int64) ([]byte, error) {
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

var reqTimeout = 20 * time.Second // 由 loadConfig 后覆盖

func classifyStatus(code int, body []byte) string {
	ct := strings.ToLower(string(body))
	switch {
	case code == 404:
		if strings.Contains(ct, "<html") || apiErrMsg(body) == "" && !strings.HasPrefix(strings.TrimSpace(ct), "{") {
			return "非Registry接口(网页404)"
		}
		return "无此镜像(404)"
	case code == 429:
		return "被限流(429)"
	case code == 403:
		return "被拒绝(403)"
	case code == 401:
		return "认证失败(401)"
	case code == 400:
		if m := apiErrMsg(body); m != "" {
			return "请求错误: " + m
		}
		return "请求错误(400)"
	case code >= 500:
		return fmt.Sprintf("服务端错误(%d)", code)
	}
	if msg := apiErrMsg(body); msg != "" {
		return fmt.Sprintf("HTTP %d: %s", code, msg)
	}
	return fmt.Sprintf("HTTP %d", code)
}

type parsedManifest struct {
	MediaType string `json:"mediaType"`
	Layers    []struct {
		Digest string `json:"digest"`
		Size   int64  `json:"size"`
	} `json:"layers"`
	Manifests []struct {
		Digest   string `json:"digest"`
		Platform *struct {
			OS           string `json:"os"`
			Architecture string `json:"architecture"`
		} `json:"platform"`
	} `json:"manifests"`
	FSLayers []struct {
		BlobSum string `json:"blobSum"`
	} `json:"fsLayers"`
}

func pickPlatform(m *parsedManifest) string {
	wantArch := runtime.GOARCH
	var linuxFirst, match string
	for _, mm := range m.Manifests {
		if mm.Platform == nil {
			continue
		}
		if mm.Platform.OS == "unknown" {
			continue
		}
		if mm.Platform.OS != "linux" {
			continue
		}
		if linuxFirst == "" {
			linuxFirst = mm.Digest
		}
		if mm.Platform.Architecture == wantArch && match == "" {
			match = mm.Digest
		}
	}
	if match != "" {
		return match
	}
	if linuxFirst != "" {
		return linuxFirst
	}
	if len(m.Manifests) > 0 {
		return m.Manifests[0].Digest
	}
	return ""
}

// getLayers 主入口：处理 索引→平台清单→层列表，认证全程透传
func getLayers(ctx context.Context, client *http.Client, base, repo, ref string) (*pullInfo, int64, string, error) {
	// 第一次：原始清单
	raw, ms, token, status, err := manifestRaw(ctx, client, base, repo, ref, "")
	if err != nil {
		return nil, ms, status, err
	}
	var m parsedManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, ms, "非Registry接口(网页)", errors.New("not json")
	}

	// 多架构索引 → 选一个平台清单
	if len(m.Manifests) > 0 && len(m.Layers) == 0 {
		digest := pickPlatform(&m)
		if digest == "" {
			return nil, ms, "清单里没有linux镜像", errors.New("no linux manifest")
		}
		raw2, ms2, token2, status2, err2 := manifestRaw(ctx, client, base, repo, digest, token)
		if err2 != nil {
			return nil, ms + ms2, status2, err2
		}
		if err := json.Unmarshal(raw2, &m); err != nil {
			return nil, ms + ms2, "清单解析失败", err
		}
		ms += ms2
		token = token2
		_ = ms2
	}

	pi := &pullInfo{repo: repo, token: token, mediaType: m.MediaType}
	if len(m.Layers) > 0 {
		for _, l := range m.Layers {
			pi.layers = append(pi.layers, layerInfo{digest: l.Digest, size: l.Size})
		}
	} else if len(m.FSLayers) > 0 {
		pi.schema1 = true
		for _, l := range m.FSLayers {
			pi.layers = append(pi.layers, layerInfo{digest: l.BlobSum, size: -1})
		}
	} else {
		return nil, ms, "无法识别的清单格式", errors.New("bad manifest")
	}
	if len(pi.layers) == 0 {
		return nil, ms, "清单没有层", errors.New("no layers")
	}
	return pi, ms, "", nil
}

// manifestRaw 拉原始清单字节，处理 401→token→重试；返回 token
func manifestRaw(ctx context.Context, client *http.Client, base, repo, ref, token string) ([]byte, int64, string, string, error) {
	start := time.Now()
	u := fmt.Sprintf("%s/v2/%s/manifests/%s", base, repo, ref)
	for attempt := 0; attempt < 3; attempt++ {
		resp, cancel, err := doReq(ctx, client, u, token, acceptHdr)
		if err != nil {
			return nil, 0, token, "", err
		}
		body, rerr := readBody(resp, 512<<10)
		cancel()
		if rerr != nil {
			return nil, 0, token, "", rerr
		}
		switch {
		case resp.StatusCode == 200:
			return body, time.Since(start).Milliseconds(), token, "", nil
		case resp.StatusCode == 401:
			ch := parseChallenge(resp.Header.Get("Www-Authenticate"))
			if ch.basic {
				return nil, time.Since(start).Milliseconds(), token, "需要账号密码(Basic)", errors.New("basic")
			}
			t, err := getToken(client, ch, repo)
			if err != nil {
				return nil, time.Since(start).Milliseconds(), token, "认证失败: " + clip(err.Error(), 40), err
			}
			token = t
		default:
			return nil, time.Since(start).Milliseconds(), token,
				classifyStatus(resp.StatusCode, body), fmt.Errorf("HTTP %d", resp.StatusCode)
		}
	}
	return nil, time.Since(start).Milliseconds(), token, "认证重试用尽", errors.New("retries")
}

// ---------------- 下载测速 ----------------

type dlResult struct {
	bytes   int64
	elapsed time.Duration // 请求发出到最后一字节
	errMsg  string
}

func downloadOnce(ctx context.Context, client *http.Client, pi *pullInfo, base string) dlResult {
	var total int64
	var totalDur time.Duration
	for _, l := range pi.layers {
		u := fmt.Sprintf("%s/v2/%s/blobs/%s", base, pi.repo, l.digest)
		d := fetchBlob(ctx, client, u, &pi.token, pi.repo)
		totalDur += d.dur
		total += d.bytes
		if d.errMsg != "" {
			return dlResult{bytes: total, elapsed: totalDur, errMsg: d.errMsg}
		}
	}
	return dlResult{bytes: total, elapsed: totalDur}
}

type blobRes struct {
	bytes  int64
	dur    time.Duration
	errMsg string
}

func fetchBlob(ctx context.Context, client *http.Client, u string, token *string, repo string) blobRes {
	start := time.Now()
	for attempt := 0; attempt < 3; attempt++ {
		reqCtx, cancel := context.WithTimeout(ctx, dlTimeout)
		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, u, nil)
		if err != nil {
			cancel()
			return blobRes{errMsg: "构造请求失败"}
		}
		req.Header.Set("User-Agent", uaHdr)
		if *token != "" {
			req.Header.Set("Authorization", "Bearer "+*token)
		}
		resp, err := client.Do(req)
		if err != nil {
			cancel()
			return blobRes{dur: time.Since(start), errMsg: classifyNetErr(err)}
		}
		if resp.StatusCode == 401 {
			ch := parseChallenge(resp.Header.Get("Www-Authenticate"))
			resp.Body.Close()
			cancel()
			if ch.basic {
				return blobRes{dur: time.Since(start), errMsg: "下载需要认证"}
			}
			t, err := getToken(client, ch, repo)
			if err != nil {
				return blobRes{dur: time.Since(start), errMsg: "下载认证失败"}
			}
			*token = t
			continue
		}
		if resp.StatusCode != 200 {
			msg := classifyStatus(resp.StatusCode, nil)
			body, _ := readBody(resp, 4096)
			if m := apiErrMsg(body); m != "" {
				msg = m
			}
			cancel()
			return blobRes{dur: time.Since(start), errMsg: msg}
		}
		n, cerr := io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		cancel()
		dur := time.Since(start)
		if cerr != nil {
			return blobRes{bytes: n, dur: dur, errMsg: classifyNetErr(cerr)}
		}
		return blobRes{bytes: n, dur: dur}
	}
	return blobRes{dur: time.Since(start), errMsg: "认证重试用尽"}
}

var dlTimeout = 120 * time.Second

// runSpeed 对单个镜像源完成测速（可重复下载凑流量）
func runSpeed(ctx context.Context, client *http.Client, base string, pi *pullInfo, minMB float64) dlResult {
	var res dlResult
	passes := 0
	for {
		one := downloadOnce(ctx, client, pi, base)
		res.bytes += one.bytes
		res.elapsed += one.elapsed
		if one.errMsg != "" {
			res.errMsg = one.errMsg
			return res
		}
		passes++
		if minMB <= 0 || float64(res.bytes) >= minMB*1048576 || passes >= 30 {
			return res
		}
	}
}

// ---------------- 结果 ----------------

type Result struct {
	Idx         int
	Mirror      string
	Host        string
	OK          bool // 清单成功
	SpeedOK     bool // 下载也成功
	Status      string
	Detail      string
	ManifestMs  int64
	DownloadSec float64
	Bytes       int64
	Speed       float64 // MB/s
	Layers      int
	Elapsed     time.Duration
}

func hostOf(m string) string {
	u, err := url.Parse(m)
	if err != nil {
		return m
	}
	return u.Host
}

// ---------------- 输出 ----------------

var (
	outMu    sync.Mutex
	colorOn  bool
	jsonMode bool // -json：只输出 NDJSON 事件，人类可读输出全部静默
)

func pln(s string) {
	if jsonMode {
		return
	}
	outMu.Lock()
	defer outMu.Unlock()
	os.Stdout.Write(encode(s))
	os.Stdout.Write([]byte("\r\n"))
}

// emit 输出一行 NDJSON 事件（仅 -json 模式，供 GUI 解析）
func emit(e map[string]interface{}) {
	if !jsonMode {
		return
	}
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	outMu.Lock()
	defer outMu.Unlock()
	os.Stdout.Write(b)
	os.Stdout.Write([]byte("\n"))
}

func col(code, s string) string {
	if !colorOn {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func dispWidth(s string) int {
	w := 0
	inEsc := false
	for _, r := range s {
		if inEsc { // 跳过 ANSI 颜色码，不参与宽度计算
			if r == 'm' {
				inEsc = false
			}
			continue
		}
		if r == '\x1b' {
			inEsc = true
			continue
		}
		if isWideRune(r) {
			w += 2
		} else {
			w++
		}
	}
	return w
}

func isWideRune(r rune) bool {
	switch {
	case r >= 0x1100 && r <= 0x115F,
		r >= 0x2E80 && r <= 0x303E,
		r >= 0x3041 && r <= 0x33FF,
		r >= 0x3400 && r <= 0x4DBF,
		r >= 0x4E00 && r <= 0x9FFF,
		r >= 0xA000 && r <= 0xA4CF,
		r >= 0xAC00 && r <= 0xD7A3,
		r >= 0xF900 && r <= 0xFAFF,
		r >= 0xFE10 && r <= 0xFE19,
		r >= 0xFE30 && r <= 0xFE6F,
		r >= 0xFF00 && r <= 0xFF60,
		r >= 0xFFE0 && r <= 0xFFE6,
		r >= 0x20000 && r <= 0x3FFFD:
		return true
	}
	return false
}

func pad(s string, width int) string {
	for dispWidth(s) < width {
		s += " "
	}
	return s
}

func padL(s string, width int) string {
	for dispWidth(s) < width {
		s = " " + s
	}
	return s
}

func fmtBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.2fGB", float64(n)/float64(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.2fMB", float64(n)/float64(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(n)/float64(1<<10))
	}
	return fmt.Sprintf("%dB", n)
}

func fmtDur(sec float64) string {
	switch {
	case sec >= 60:
		return fmt.Sprintf("%dm%02ds", int(sec)/60, int(sec)%60)
	case sec >= 10:
		return fmt.Sprintf("%.1fs", sec)
	case sec >= 1:
		return fmt.Sprintf("%.2fs", sec)
	}
	return fmt.Sprintf("%.0fms", sec*1000)
}

// ---------------- 主流程 ----------------

// encWriter 让 flag 帮助信息按控制台编码输出（否则 cmd 下中文乱码）
type encWriter struct{}

func (encWriter) Write(p []byte) (int, error) {
	if _, err := os.Stderr.Write(encode(string(p))); err != nil {
		return 0, err
	}
	return len(p), nil
}

func main() {
	initConsole("utf8") // 默认 UTF-8；加载配置后按 console_encoding 再切
	colorOn = enableColor() && os.Getenv("NO_COLOR") == ""
	reqTimeout = 20 * time.Second
	flag.CommandLine.SetOutput(encWriter{})

	imageFlag := flag.String("image", "", "测试镜像（默认读 config.txt）")
	listFlag := flag.String("list", "", "镜像列表文件（默认 exe 同目录 mirrors.txt）")
	noPause := flag.Bool("nopause", false, "结束时不等待回车")
	noCSV := flag.Bool("nocsv", false, "不保存 CSV 结果")
	jsonFlag := flag.Bool("json", false, "输出 NDJSON 事件行（供图形界面解析，强制 UTF-8）")
	showVer := flag.Bool("version", false, "显示版本")
	flag.Parse()
	jsonMode = *jsonFlag

	if *showVer {
		fmt.Println(appName, appVersion)
		return
	}

	exePath, err := os.Executable()
	if err != nil {
		exePath = "."
	}
	dir := filepath.Dir(exePath)

	cfgPath := filepath.Join(dir, "config.txt")
	if _, err := os.Stat(cfgPath); err != nil {
		os.WriteFile(cfgPath, []byte(defaultConfigText), 0644)
	}
	cfg, _ := loadConfig(cfgPath)
	if *imageFlag != "" {
		cfg.Image = *imageFlag
	}
	if *noCSV {
		cfg.SaveCSV = false
	}
	reqTimeout = cfg.Timeout
	dlTimeout = cfg.DownloadTimeout

	// 最终输出编码：-json 强制 UTF-8；否则按配置 console_encoding
	mode := cfg.ConsoleEnc
	if jsonMode {
		mode = "utf8"
	}
	initConsole(mode)
	if jsonMode {
		colorOn = false
	}

	listPath := *listFlag
	if listPath == "" {
		listPath = filepath.Join(dir, "mirrors.txt")
	}
	if _, err := os.Stat(listPath); err != nil {
		os.WriteFile(listPath, []byte(defaultMirrorsText), 0644)
	}
	mirrors, err := loadMirrors(listPath)
	if err != nil || len(mirrors) == 0 {
		msg := "读取镜像列表失败: " + listPath
		pln(col("31", msg))
		if err != nil {
			pln("  " + err.Error())
			msg += " (" + err.Error() + ")"
		}
		emit(map[string]interface{}{"t": "error", "msg": msg})
		pause(cfg, *noPause)
		return
	}

	// HTTP 客户端
	client := &http.Client{
		Transport: &http.Transport{
			Proxy:               proxyFunc(cfg.Proxy),
			TLSHandshakeTimeout: 10 * time.Second,
			MaxIdleConns:        64,
			IdleConnTimeout:     30 * time.Second,
		},
	}

	repo, ref := splitImage(cfg.Image)

	emit(map[string]interface{}{"t": "start", "image": cfg.Image, "repo": repo, "ref": ref,
		"total": len(mirrors), "list": listPath,
		"time": time.Now().Format("2006-01-02 15:04:05")})

	// ---------- 头部 ----------
	banner := "==== " + appName + " v" + appVersion + " : Docker 镜像加速测速（免安装 Docker） ===="
	pln(col("1;36", banner))
	pln("测试镜像 : " + cfg.Image + "   (仓库 " + repo + ", 标签 " + ref + ")")
	pln("镜像列表 : " + filepath.Base(listPath) + " (" + itoa(len(mirrors)) + " 个)")
	pln("开始时间 : " + time.Now().Format("2006-01-02 15:04:05"))
	if cfg.Proxy != "" {
		pln("代理     : " + cfg.Proxy)
	}
	pln(col("90", "阶段1 连通性+镜像清单检测（并发 "+itoa(cfg.Phase1)+"）…"))

	// ---------- 阶段1 ----------
	results := make([]Result, len(mirrors))
	var done int64
	var wg sync.WaitGroup
	sem := make(chan struct{}, cfg.Phase1)
	t0 := time.Now()
	for i, m := range mirrors {
		wg.Add(1)
		go func(i int, m string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			r := Result{Idx: i, Mirror: m, Host: hostOf(m)}
			st := time.Now()
			pi, ms, note, err := getLayers(context.Background(), client, m, repo, ref)
			r.Elapsed = time.Since(st)
			desc := ""
			if err != nil {
				r.OK = false
				r.Status = "失败"
				if note == "" {
					note = classifyNetErr(err)
				}
				r.Detail = note
				desc = note
			} else {
				r.OK = true
				r.Status = "可达"
				r.ManifestMs = ms
				r.Layers = len(pi.layers)
				r.Detail = "层 " + itoa(len(pi.layers))
				if pi.schema1 {
					r.Detail += " (旧格式)"
				}
				desc = fmt.Sprintf("清单 %dms, %s", ms, r.Detail)
			}
			results[i] = r
			n := atomic.AddInt64(&done, 1)
			emit(map[string]interface{}{"t": "p1", "n": int(n), "total": len(mirrors),
				"host": r.Host, "url": m, "ok": r.OK, "ms": r.ManifestMs,
				"layers": r.Layers, "detail": r.Detail})
			mark := col("32", "[OK]  ")
			if !r.OK {
				mark = col("31", "[FAIL]")
			}
			pln(fmt.Sprintf("  %s %s %s",
				pad(itoa(int(n))+"/"+itoa(len(mirrors)), 7),
				mark,
				pad(r.Host, 28)+desc))
		}(i, m)
	}
	wg.Wait()
	phase1Dur := time.Since(t0)

	// ---------- 阶段2 ----------
	var reachable []int
	for i, r := range results {
		if r.OK {
			reachable = append(reachable, i)
		}
	}
	if len(reachable) > 0 {
		pln("")
		pln(col("90", fmt.Sprintf("阶段2 实际拉取测速（%d 个可达源，并发 %d，%s）…",
			len(reachable), cfg.Speed, cfg.Image)))
		var d2, d2done int64
		sem2 := make(chan struct{}, cfg.Speed)
		var wg2 sync.WaitGroup
		var mu sync.Mutex
		for _, i := range reachable {
			wg2.Add(1)
			go func(i int) {
				defer wg2.Done()
				sem2 <- struct{}{}
				defer func() { <-sem2 }()
				r := &results[i]
				st := time.Now()
				pi, ms, note, err := getLayers(context.Background(), client, r.Mirror, repo, ref)
				if err != nil {
					mu.Lock()
					r.OK = false
					r.Status = "失败"
					if note == "" {
						note = classifyNetErr(err)
					}
					r.Detail = note
					n := atomic.AddInt64(&d2done, 1)
					emit(map[string]interface{}{"t": "p2", "n": int(n), "total": len(reachable),
						"host": r.Host, "url": r.Mirror, "ok": false, "detail": r.Detail})
					pln(fmt.Sprintf("  %s [FAIL] %-28s %s",
						pad(itoa(int(n))+"/"+itoa(len(reachable)), 7), r.Host, r.Detail))
					mu.Unlock()
					atomic.AddInt64(&d2, 1)
					return
				}
				r.ManifestMs = ms
				res := runSpeed(context.Background(), client, r.Mirror, pi, cfg.MinTestMB)
				r.Elapsed = time.Since(st)
				r.Bytes = res.bytes
				r.DownloadSec = res.elapsed.Seconds()
				if res.errMsg != "" || res.bytes == 0 {
					r.SpeedOK = false
					r.Status = "受限"
					if res.errMsg == "" {
						res.errMsg = "0字节"
					}
					r.Detail = "层下载失败: " + res.errMsg
				} else {
					r.SpeedOK = true
					r.Status = "可用"
					r.Speed = float64(res.bytes) / 1048576 / res.elapsed.Seconds()
					r.Detail = "层 " + itoa(r.Layers)
				}
				n := atomic.AddInt64(&d2done, 1)
				emit(map[string]interface{}{"t": "p2", "n": int(n), "total": len(reachable),
					"host": r.Host, "url": r.Mirror, "ok": r.SpeedOK, "ms": r.ManifestMs,
					"bytes": r.Bytes, "sec": r.DownloadSec, "speed": r.Speed, "detail": r.Detail})
				mark := col("32", "[OK]  ")
				if !r.SpeedOK {
					mark = col("33", "[PART]")
				}
				mu.Lock()
				pln(fmt.Sprintf("  %s %s %-28s %-12s %8s %s",
					pad(itoa(int(n))+"/"+itoa(len(reachable)), 7),
					mark, r.Host,
					if0(r.SpeedOK, fmt.Sprintf("%.2f MB/s", r.Speed), r.Detail),
					fmtBytes(res.bytes), fmtDur(r.DownloadSec)))
				mu.Unlock()
				atomic.AddInt64(&d2, 1)
			}(i)
		}
		wg2.Wait()
	}

	// ---------- 汇总表 ----------
	pln("")
	printTable(results)

	// 统计
	var okN, failN int
	var best Result
	for _, r := range results {
		if r.SpeedOK {
			okN++
			if r.Speed > best.Speed {
				best = r
			}
		} else {
			failN++
		}
	}
	pln(strings.Repeat("-", 78))
	summary := fmt.Sprintf("可用 %d 个 / 不可用 %d 个，阶段1耗时 %s，总耗时 %s",
		okN, failN, fmtDur(phase1Dur.Seconds()), fmtDur(time.Since(t0).Seconds()))
	if best.Host != "" {
		summary += "  |  最快: " + best.Host + " " + fmt.Sprintf("%.2f MB/s", best.Speed)
	}
	if okN == 0 {
		pln(col("31", summary))
	} else {
		pln(col("1;32", summary))
	}
	pln(col("90", "提示: speed_concurrency=1 时各源顺序拉取，速度互不抢带宽；换 image= 可测大镜像"))

	// ---------- CSV ----------
	csvPath := ""
	if cfg.SaveCSV {
		name := "results_" + time.Now().Format("20060102_150405") + ".csv"
		p := filepath.Join(dir, name)
		if err := saveCSV(p, results, cfg); err == nil {
			csvPath = p
			pln("结果已保存: " + p)
		} else {
			pln(col("33", "CSV 保存失败: "+err.Error()))
		}
	}

	emit(map[string]interface{}{"t": "done", "ok": okN, "fail": failN,
		"phase1_sec": phase1Dur.Seconds(), "total_sec": time.Since(t0).Seconds(),
		"best": best.Host, "best_speed": best.Speed, "csv": csvPath})

	pause(cfg, *noPause || len(os.Args) > 1)
}

func if0(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

func pause(cfg Config, forceNo bool) {
	if jsonMode || forceNo || !cfg.Pause {
		return
	}
	if !stdinIsConsole() {
		return
	}
	pln("")
	pln("按回车键退出…")
	buf := make([]byte, 1)
	os.Stdin.Read(buf)
}

func splitImage(img string) (repo, ref string) {
	img = strings.TrimSpace(img)
	if img == "" {
		return "library/busybox", "latest"
	}
	// 拆 tag（最后一个 ':' 在最后一个 '/' 之后才当 tag）
	ref = "latest"
	if i := strings.LastIndex(img, ":"); i > 0 && strings.LastIndex(img, "/") < i {
		ref = img[i+1:]
		img = img[:i]
	}
	if !strings.Contains(img, "/") {
		img = "library/" + img
	}
	return img, ref
}

func proxyFunc(p string) func(*http.Request) (*url.URL, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return http.ProxyFromEnvironment
	}
	u, err := url.Parse(p)
	if err != nil {
		return http.ProxyFromEnvironment
	}
	return http.ProxyURL(u)
}

func itoa(n int) string { return fmt.Sprintf("%d", n) }

// clip 按 rune 截断错误信息，避免截断 UTF-8 中间
func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func printTable(results []Result) {
	head := " " + pad("序", 4) + pad("镜像源", 30) + pad("状态", 10) +
		pad("清单延迟", 10) + pad("下载耗时", 10) + pad("大小", 10) + pad("速度", 14) + "说明"
	pln(col("1", head))
	pln(" " + strings.Repeat("-", 88))

	// 可用的按速度降序，不可用的按原序排后面
	var okList, badList []Result
	for _, r := range results {
		if r.SpeedOK {
			okList = append(okList, r)
		} else {
			badList = append(badList, r)
		}
	}
	sort.Slice(okList, func(i, j int) bool { return okList[i].Speed > okList[j].Speed })
	sort.Slice(badList, func(i, j int) bool { return badList[i].Idx < badList[j].Idx })

	line := func(r Result, rank int) {
		status := r.Status
		switch {
		case r.SpeedOK:
			status = col("1;32", "可用")
		case r.OK:
			status = col("33", "受限")
		default:
			status = col("31", "失败")
		}
		man := "-"
		if r.OK {
			man = itoa(int(r.ManifestMs)) + "ms"
		}
		dl, size, speed := "-", "-", "-"
		if r.DownloadSec > 0 {
			dl = fmtDur(r.DownloadSec)
			size = fmtBytes(r.Bytes)
		}
		if r.SpeedOK {
			speed = fmt.Sprintf("%.2f MB/s", r.Speed)
		}
		pln(" " + padL(itoa(rank), 3) + " " + pad(r.Host, 29) + pad(status, 10) +
			pad(man, 10) + pad(dl, 10) + pad(size, 10) + pad(speed, 14) + r.Detail)
	}
	rank := 0
	for _, r := range okList {
		rank++
		line(r, rank)
	}
	for _, r := range badList {
		rank++
		line(r, rank)
	}
}

func saveCSV(path string, results []Result, cfg Config) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	// UTF-8 BOM，Excel 直接双击不乱码
	f.Write([]byte{0xEF, 0xBB, 0xBF})
	w := csv.NewWriter(f)
	defer w.Flush()
	w.Write([]string{"序", "镜像源", "状态", "说明", "清单延迟ms", "下载耗时s", "大小Bytes", "速度MB/s", "层数", "测试镜像", "时间"})
	for i, r := range results {
		speed := ""
		if r.SpeedOK {
			speed = fmt.Sprintf("%.2f", r.Speed)
		}
		man := ""
		if r.OK {
			man = itoa(int(r.ManifestMs))
		}
		dl := ""
		if r.DownloadSec > 0 {
			dl = fmt.Sprintf("%.2f", r.DownloadSec)
		}
		size := ""
		if r.Bytes > 0 {
			size = itoa(int(r.Bytes))
		}
		w.Write([]string{
			itoa(i + 1), r.Mirror, r.Status, r.Detail,
			man, dl, size, speed, itoa(r.Layers),
			cfg.Image, time.Now().Format("2006-01-02 15:04:05"),
		})
	}
	return nil
}
