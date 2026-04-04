package main

import (
	"context"
	"crypto/tls"
	"embed"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"subconv/config"
	"subconv/converter"
	"subconv/server"
)

//go:embed web/index.html
var webFS embed.FS

var version = "dev"

func main() {
	configPath := flag.String("c", "", "配置文件路径（默认为程序所在目录的 config.yaml）")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	slog.Info("SubConv 启动", "version", version)

	// 确定程序所在目录，所有运行时文件都放在这里
	exePath, err := os.Executable()
	if err != nil {
		slog.Error("获取程序路径失败", "err", err)
		os.Exit(1)
	}
	baseDir := filepath.Dir(exePath)

	// 切换工作目录到程序所在目录
	if err := os.Chdir(baseDir); err != nil {
		slog.Error("切换工作目录失败", "err", err)
		os.Exit(1)
	}
	slog.Info("工作目录", "path", baseDir)

	cfgPath := *configPath
	if cfgPath == "" {
		cfgPath = filepath.Join(baseDir, "config.yaml")
	}

	if err := config.Load(cfgPath); err != nil {
		slog.Error("加载配置失败", "err", err)
		os.Exit(1)
	}

	cfg := config.Get()
	slog.Info("配置加载完成", "listen", cfg.Listen, "subscriptions", len(cfg.Subscriptions))

	html, err := webFS.ReadFile("web/index.html")
	if err != nil {
		slog.Error("读取Web资源失败", "err", err)
		os.Exit(1)
	}

	store := server.NewStore()
	srv := server.New(store, html)

	// 启动后台更新
	ctx, cancel := context.WithCancel(context.Background())
	go runUpdater(ctx, store)

	// 监听退出信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// 启动HTTP服务（非阻塞）
	go func() {
		var err error
		if cfg.TLSCert != "" && cfg.TLSKey != "" {
			// 预检证书文件是否可读
			if _, e := os.Stat(cfg.TLSCert); e != nil {
				slog.Error("TLS证书文件不可访问", "path", cfg.TLSCert, "err", e)
				os.Exit(1)
			}
			if _, e := os.Stat(cfg.TLSKey); e != nil {
				slog.Error("TLS私钥文件不可访问", "path", cfg.TLSKey, "err", e)
				os.Exit(1)
			}
			err = srv.StartTLS(cfg.Listen, cfg.TLSCert, cfg.TLSKey)
		} else {
			err = srv.Start(cfg.Listen)
		}
		if err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP服务异常退出", "err", err)
			os.Exit(1)
		}
	}()

	<-quit
	slog.Info("收到退出信号，正在关闭...")
	cancel()
	srv.Shutdown(5 * time.Second)
	slog.Info("SubConv 已停止")
}

func runUpdater(ctx context.Context, store *server.Store) {
	// 首次启动立即更新
	doUpdate(store)

	cfg := config.Get()
	interval := time.Duration(cfg.UpdateInterval) * time.Minute
	if interval <= 0 {
		slog.Info("自动更新已禁用，等待手动刷新")
		for {
			select {
			case <-ctx.Done():
				return
			case <-store.RefreshChan():
				doUpdate(store)
			}
		}
	}

	store.SetNextUpdate(time.Now().Add(interval))
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			doUpdate(store)
			store.SetNextUpdate(time.Now().Add(interval))
		case <-store.RefreshChan():
			doUpdate(store)
			store.SetNextUpdate(time.Now().Add(interval))
			ticker.Reset(interval)
		}
	}
}

func doUpdate(store *server.Store) {
	if !store.Updating().CompareAndSwap(false, true) {
		return
	}
	defer store.Updating().Store(false)
	defer store.NotifyDone()

	cfg := config.Get()
	if len(cfg.Subscriptions) == 0 {
		store.AddLog("无订阅链接，跳过更新")
		slog.Info("无订阅链接，跳过更新")
		return
	}

	store.AddLog(fmt.Sprintf("开始更新，共 %d 个订阅", len(cfg.Subscriptions)))
	slog.Info("开始更新订阅", "count", len(cfg.Subscriptions))

	var (
		allProxies []converter.Proxy
		allUserInfo []string
		mu         sync.Mutex
		wg         sync.WaitGroup
	)

	client := &http.Client{
		Timeout: time.Duration(cfg.Timeout) * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	for _, sub := range cfg.Subscriptions {
		wg.Add(1)
		go func(s config.Subscription) {
			defer wg.Done()
			result, err := fetchSubscription(client, s, cfg.UserAgent)
			if err != nil {
				msg := fmt.Sprintf("❌ [%s] 获取失败: %v", s.Name, err)
				store.AddLog(msg)
				slog.Error(msg)
				return
			}
			mu.Lock()
			allProxies = append(allProxies, result.proxies...)
			if result.userInfo != "" {
				allUserInfo = append(allUserInfo, result.userInfo)
				slog.Info("获取到订阅流量信息", "name", s.Name, "userinfo", result.userInfo)
			}
			mu.Unlock()
			msg := fmt.Sprintf("✅ [%s] 获取成功，%d 个节点", s.Name, len(result.proxies))
			store.AddLog(msg)
			slog.Info(msg)
		}(sub)
	}
	wg.Wait()

	// 去重（按 server:port:type 去重）
	allProxies = dedup(allProxies)

	store.SetProxies(allProxies)
	// 透传上游订阅信息（多个订阅时取第一个有效值）
	if len(allUserInfo) > 0 {
		store.SetUserInfo(allUserInfo[0])
	}
	msg := fmt.Sprintf("更新完成，共 %d 个节点", len(allProxies))
	store.AddLog(msg)
	slog.Info(msg)
}

type fetchResult struct {
	proxies  []converter.Proxy
	userInfo string // Subscription-Userinfo header
}

func fetchSubscription(client *http.Client, sub config.Subscription, ua string) (*fetchResult, error) {
	req, err := http.NewRequest("GET", sub.URL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", ua)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20)) // 10MB limit
	if err != nil {
		return nil, err
	}

	return &fetchResult{
		proxies:  converter.ParseContent(body, sub.Name),
		userInfo: resp.Header.Get("Subscription-Userinfo"),
	}, nil
}

func dedup(proxies []converter.Proxy) []converter.Proxy {
	seen := make(map[string]struct{})
	result := make([]converter.Proxy, 0, len(proxies))
	for _, p := range proxies {
		key := fmt.Sprintf("%s:%d:%s:%s", p.Server, p.Port, p.Type, p.UUID+p.Password)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, p)
	}
	return result
}
