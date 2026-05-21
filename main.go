package main

import (
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	// "gopkg.in/natefinch/lumberjack.v2"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

// --- config ---

var (
	listenAddr    string
	tlsCert       string
	tlsKey        string
	// daemonize     bool
	// logFile       string
	dockerCfgPath string
)

const (
	dockerHub = "docker.fnnas.com"
	authURL   = "https://docker.fnnas.com"
)

// --- dynamic upstream headers ---

type dockerConfig struct {
	HttpHeaders map[string]string `json:"HttpHeaders"`
}

func setUpstreamHeaders(req *http.Request) {
	if dockerCfgPath == "" {
		return
	}
	data, err := os.ReadFile(dockerCfgPath)
	if err != nil {
		return
	}
	var cfg dockerConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return
	}
	for k, v := range cfg.HttpHeaders {
		req.Header.Set(k, v)
	}
}

// --- regex ---

var (
	v2ShortPathRegex = regexp.MustCompile(`^/v2/[^/]+/[^/]+/[^/]+$`)
	v2LibraryRegex   = regexp.MustCompile(`^/v2/library`)
	repoExtractRegex = regexp.MustCompile(`^/v2/(.+?)(?:/(manifests|blobs|tags)/)`)
	repoExtractList  = regexp.MustCompile(`^/v2/(.+?)/tags/list`)
)

// --- http clients ---

var registryClient = &http.Client{
	Timeout: 300 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig:       &tls.Config{},
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   50,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

var downloadClient = &http.Client{
	Timeout: 600 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig:       &tls.Config{},
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   50,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: 120 * time.Second,
	},
}

// --- token cache ---

type tokenEntry struct {
	token   string
	expires time.Time
}

var (
	tokenCacheMu sync.RWMutex
	tokenCache   = make(map[string]tokenEntry)
)

func getCachedToken(repo string) (string, bool) {
	tokenCacheMu.RLock()
	defer tokenCacheMu.RUnlock()
	if e, ok := tokenCache[repo]; ok && time.Now().Before(e.expires) {
		return e.token, true
	}
	return "", false
}

func setCachedToken(repo, token string, ttl time.Duration) {
	tokenCacheMu.Lock()
	defer tokenCacheMu.Unlock()
	tokenCache[repo] = tokenEntry{token: token, expires: time.Now().Add(ttl)}
}

// --- main ---

func main() {
	flag.StringVar(&listenAddr, "addr", ":5001", "监听地址")
	flag.StringVar(&tlsCert, "tls-cert", "", "TLS 证书路径")
	flag.StringVar(&tlsKey, "tls-key", "", "TLS 私钥路径")
	// flag.BoolVar(&daemonize, "d", false, "后台守护进程模式")
	// flag.StringVar(&logFile, "log", "docker-proxy.log", "日志文件路径")
	flag.StringVar(&dockerCfgPath, "docker-config", "", "Docker config.json 路径（包含上游认证头）")
	flag.Parse()

	// 确定配置文件路径：命令行传入 > $HOME/.docker/config.json > /app/config.json
	if dockerCfgPath == "" {
		if home, err := os.UserHomeDir(); err == nil {
			p := home + "/.docker/config.json"
			if _, err := os.Stat(p); err == nil {
				dockerCfgPath = p
			}
		}
	}
	if dockerCfgPath == "" {
		if _, err := os.Stat("/app/config.json"); err == nil {
			dockerCfgPath = "/app/config.json"
		}
	}
	if dockerCfgPath != "" {
		log.Printf("使用配置文件: %s", dockerCfgPath)
	}

	// if daemonize {
	// 	runDaemon()
	// 	return
	// }
	// if os.Getenv("_DOCKER_PROXY_CHILD") == "1" {
	// 	setupLogging()
	// }

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleRequest)

	server := &http.Server{
		Addr:         listenAddr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 300 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
		<-ch
		log.Println("正在关闭...")
		server.Close()
	}()

	if tlsCert != "" && tlsKey != "" {
		log.Printf("Docker 代理已启动 (HTTPS) %s\n", listenAddr)
		if err := server.ListenAndServeTLS(tlsCert, tlsKey); err != http.ErrServerClosed {
			log.Fatalf("服务异常: %v", err)
		}
	} else {
		log.Printf("Docker 代理已启动 (HTTP) %s\n", listenAddr)
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("服务异常: %v", err)
		}
	}
}

// --- daemon (disabled) ---

// func runDaemon() {
// 	var args []string
// 	for _, a := range os.Args[1:] {
// 		if a != "-d" {
// 			args = append(args, a)
// 		}
// 	}
// 	proc, err := os.StartProcess(os.Args[0], append([]string{os.Args[0]}, args...), &os.ProcAttr{
// 		Dir:   ".",
// 		Env:   append(os.Environ(), "_DOCKER_PROXY_CHILD=1"),
// 		Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
// 		Sys:   &syscall.SysProcAttr{Setsid: true},
// 	})
// 	if err != nil {
// 		log.Fatalf("启动守护进程失败: %v", err)
// 	}
// 	fmt.Printf("Docker 代理已在后台启动, PID: %d\n", proc.Pid)
// 	if f, err := os.Create("docker-proxy.pid"); err == nil {
// 		fmt.Fprintf(f, "%d\n", proc.Pid)
// 		f.Close()
// 	}
// 	os.Exit(0)
// }

// func setupLogging() {
// 	lj := &lumberjack.Logger{
// 		Filename:   logFile,
// 		MaxSize:    100,
// 		MaxBackups: 5,
// 		MaxAge:     30,
// 		Compress:   true,
// 	}
// 	log.SetOutput(lj)
// 	r, w, _ := os.Pipe()
// 	os.Stdout = w
// 	os.Stderr = w
// 	go io.Copy(lj, r)
// }

// --- request router ---

func handleRequest(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case path == "/v2/" || path == "/v2":
		handleV2Ping(w)
	case strings.HasPrefix(path, "/v2/"):
		handleV2(w, r)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// --- /v2/ ping ---

func handleV2Ping(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Docker-Distribution-Api-Version", "registry/2.0")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("{}"))
}

// --- /v2/<name>/... ---

func handleV2(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	rawQuery := r.URL.RawQuery

	// 官方镜像自动补 library/ 前缀
	if v2ShortPathRegex.MatchString(path) && !v2LibraryRegex.MatchString(path) {
		if parts := strings.SplitN(path, "/v2/", 2); len(parts) == 2 {
			path = "/v2/library/" + parts[1]
		}
	}

	// 打印拉取镜像信息
	if repo := extractRepo(path); repo != "" {
		if strings.Contains(path, "/manifests/") {
			tag := path[strings.LastIndex(path, "/")+1:]
			log.Printf("[PULL] %s -> %s:%s", r.RemoteAddr, repo, tag)
		}
	}

	// 获取 token
	needsAuth := strings.Contains(path, "/manifests/") ||
		strings.Contains(path, "/blobs/") ||
		strings.Contains(path, "/tags/")

	var token string
	if needsAuth {
		if repo := extractRepo(path); repo != "" {
			var err error
			token, err = getToken(repo)
			if err != nil {
				log.Printf("获取 token 失败 (repo=%s): %v", repo, err)
			}
		}
	}

	target := fmt.Sprintf("https://%s%s", dockerHub, path)
	if rawQuery != "" {
		target += "?" + rawQuery
	}

	req, err := http.NewRequest(r.Method, target, r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	copySelectHeaders(req.Header, r.Header)
	req.Header.Set("Host", dockerHub)
	setUpstreamHeaders(req)

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	} else if auth := r.Header.Get("Authorization"); auth != "" {
		req.Header.Set("Authorization", auth)
	}

	resp, err := registryClient.Do(req)
	if err != nil {
		log.Printf("上游请求失败: %v", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// 跟随 CDN 重定向
	if loc := resp.Header.Get("Location"); loc != "" && isRedirectCode(resp.StatusCode) {
		log.Printf("跟随重定向: %s", loc)
		handleCDNRedirect(w, r, loc)
		return
	}

	if resp.StatusCode == http.StatusUnauthorized {
		log.Printf("上游 401 (path=%s)", path)
	}

	flushResponse(w, resp)
}

// --- CDN redirect (blob download) ---

func handleCDNRedirect(w http.ResponseWriter, orig *http.Request, location string) {
	req, err := http.NewRequest(orig.Method, location, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	copyAllHeaders(req.Header, orig.Header)
	req.Header.Del("Authorization")

	resp, err := downloadClient.Do(req)
	if err != nil {
		log.Printf("CDN 下载失败: %v", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// --- token ---

func extractRepo(path string) string {
	if m := repoExtractRegex.FindStringSubmatch(path); len(m) > 1 {
		return m[1]
	}
	if m := repoExtractList.FindStringSubmatch(path); len(m) > 1 {
		return m[1]
	}
	return ""
}

func getToken(repo string) (string, error) {
	if t, ok := getCachedToken(repo); ok {
		return t, nil
	}

	tokenURL := fmt.Sprintf("%s/service/token?service=harbor-registry&scope=repository:%s:pull", authURL, repo)
	req, err := http.NewRequest("GET", tokenURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "docker-proxy/1.0")
	setUpstreamHeaders(req)

	resp, err := registryClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("auth 请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取 token 失败: %w", err)
	}

	var result struct {
		Token     string `json:"token"`
		ExpiresIn int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("解析 token 失败: %w", err)
	}
	if result.Token == "" {
		return "", fmt.Errorf("token 为空, resp=%s", string(body))
	}

	ttl := time.Duration(result.ExpiresIn) * time.Second
	if ttl <= 0 || ttl > 300*time.Second {
		ttl = 250 * time.Second
	} else {
		ttl -= 30 * time.Second
	}
	setCachedToken(repo, result.Token, ttl)
	log.Printf("token 已缓存 (repo=%s, ttl=%s)", repo, ttl)
	return result.Token, nil
}

// --- helpers ---

func copySelectHeaders(dst, src http.Header) {
	for _, k := range []string{
		"User-Agent", "Accept", "Accept-Language", "Accept-Encoding",
		"Cache-Control", "If-None-Match", "If-Modified-Since",
	} {
		if v := src.Get(k); v != "" {
			dst.Set(k, v)
		}
	}
}

func copyAllHeaders(dst, src http.Header) {
	for k, vv := range src {
		if strings.EqualFold(k, "Host") {
			continue
		}
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func flushResponse(w http.ResponseWriter, resp *http.Response) {
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func isRedirectCode(code int) bool {
	return code == 301 || code == 302 || code == 303 || code == 307 || code == 308
}
