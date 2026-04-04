package server

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"subconv/config"
	"subconv/converter"
)

// Store 线程安全的共享状态
type Store struct {
	mu          sync.RWMutex
	proxies     []converter.Proxy
	clashYAML   []byte
	lastUpdate  time.Time
	nextUpdate  time.Time
	userInfo    string // 上游 Subscription-Userinfo
	updating    atomic.Bool
	logs        []string
	logMu       sync.Mutex
	refreshChan chan struct{}
	doneMu      sync.Mutex
	doneWaiters []chan struct{}
}

func NewStore() *Store {
	return &Store{
		refreshChan: make(chan struct{}, 1),
	}
}

func (s *Store) SetProxies(proxies []converter.Proxy) {
	clashYAML, err := converter.GenerateClashConfig(proxies)
	if err != nil {
		slog.Error("生成Clash配置失败", "err", err)
		return
	}
	s.mu.Lock()
	s.proxies = proxies
	s.clashYAML = clashYAML
	s.lastUpdate = time.Now()
	s.mu.Unlock()
}

func (s *Store) GetProxies() []converter.Proxy {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]converter.Proxy, len(s.proxies))
	copy(out, s.proxies)
	return out
}

func (s *Store) GetClashYAML() []byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.clashYAML
}

func (s *Store) SetUserInfo(info string) {
	s.mu.Lock()
	s.userInfo = info
	s.mu.Unlock()
}

func (s *Store) GetUserInfo() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.userInfo
}

func (s *Store) SetNextUpdate(t time.Time) {
	s.mu.Lock()
	s.nextUpdate = t
	s.mu.Unlock()
}

func (s *Store) AddLog(msg string) {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	entry := time.Now().Format("15:04:05") + " " + msg
	s.logs = append(s.logs, entry)
	if len(s.logs) > 200 {
		s.logs = s.logs[len(s.logs)-200:]
	}
}

func (s *Store) GetLogs() []string {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	out := make([]string, len(s.logs))
	copy(out, s.logs)
	return out
}

func (s *Store) TriggerRefresh() {
	select {
	case s.refreshChan <- struct{}{}:
	default:
	}
}

func (s *Store) RefreshChan() <-chan struct{} {
	return s.refreshChan
}

func (s *Store) Updating() *atomic.Bool {
	return &s.updating
}

// TriggerRefreshAndWait 触发更新并等待完成
func (s *Store) TriggerRefreshAndWait(timeout time.Duration) {
	ch := make(chan struct{}, 1)
	s.doneMu.Lock()
	s.doneWaiters = append(s.doneWaiters, ch)
	s.doneMu.Unlock()
	s.TriggerRefresh()
	select {
	case <-ch:
	case <-time.After(timeout):
	}
}

// NotifyDone 通知所有等待者更新已完成
func (s *Store) NotifyDone() {
	s.doneMu.Lock()
	for _, ch := range s.doneWaiters {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	s.doneWaiters = nil
	s.doneMu.Unlock()
}

// Server HTTP服务器
type Server struct {
	store  *Store
	mux    *http.ServeMux
	html   []byte
	server *http.Server
}

func New(store *Store, html []byte) *Server {
	s := &Server{store: store, html: html}
	s.setupRoutes()
	return s
}

func (s *Server) Start(addr string) error {
	s.server = &http.Server{Addr: addr, Handler: s.mux}
	slog.Info("HTTP服务启动", "地址", addr)
	return s.server.ListenAndServe()
}

func (s *Server) StartTLS(addr, certFile, keyFile string) error {
	s.server = &http.Server{Addr: addr, Handler: s.mux}
	slog.Info("HTTPS服务启动", "地址", addr, "证书", certFile)
	return s.server.ListenAndServeTLS(certFile, keyFile)
}

func (s *Server) Shutdown(timeout time.Duration) {
	if s.server == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	s.server.Shutdown(ctx)
}

func (s *Server) setupRoutes() {
	s.mux = http.NewServeMux()

	// 公开路由
	s.mux.HandleFunc("GET /sub", s.handleSub)
	s.mux.HandleFunc("GET /", s.handleRoot)
	s.mux.HandleFunc("GET /admin", s.handleAdmin)

	// API 路由（需要认证）
	s.mux.HandleFunc("GET /api/status", s.withAuth(s.handleStatus))
	s.mux.HandleFunc("GET /api/nodes", s.withAuth(s.handleNodes))
	s.mux.HandleFunc("GET /api/config", s.withAuth(s.handleGetConfig))
	s.mux.HandleFunc("POST /api/config", s.withAuth(s.handleSaveConfig))
	s.mux.HandleFunc("POST /api/subscriptions", s.withAuth(s.handleAddSub))
	s.mux.HandleFunc("DELETE /api/subscriptions", s.withAuth(s.handleDeleteSub))
	s.mux.HandleFunc("POST /api/subscriptions/delete", s.withAuth(s.handleDeleteSub))
	s.mux.HandleFunc("POST /api/refresh", s.withAuth(s.handleRefresh))
	s.mux.HandleFunc("GET /api/logs", s.withAuth(s.handleLogs))
}

func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("X-API-Key")
		if key == "" {
			key = r.URL.Query().Get("key")
		}
		cfg := config.Get()
		if subtle.ConstantTimeCompare([]byte(key), []byte(cfg.APIKey)) != 1 {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "未授权"})
			return
		}
		next(w, r)
	}
}

// handleRoot 根路径重定向到管理面板
func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusFound)
}

// handleAdmin 管理面板
func (s *Server) handleAdmin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(s.html)
}

// handleSub Clash 订阅端点
func (s *Server) handleSub(w http.ResponseWriter, r *http.Request) {
	cfg := config.Get()
	if cfg.SubToken != "" {
		token := r.URL.Query().Get("token")
		if subtle.ConstantTimeCompare([]byte(token), []byte(cfg.SubToken)) != 1 {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
	}

	// Clash Verge 拉取时同步刷新上游订阅
	slog.Info("订阅拉取触发更新")
	s.store.TriggerRefreshAndWait(time.Duration(cfg.Timeout*len(cfg.Subscriptions)+5) * time.Second)

	data := s.store.GetClashYAML()
	if len(data) == 0 {
		http.Error(w, "暂无节点数据，请先添加订阅并刷新", http.StatusServiceUnavailable)
		return
	}

	// 确定配置名称: query参数 > 配置文件
	profileName := r.URL.Query().Get("name")
	if profileName == "" {
		profileName = cfg.ConfigName
	}
	if profileName == "" {
		profileName = "SubConv"
	}

	filename := profileName + ".yaml"
	w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
	// RFC 5987: ASCII fallback + UTF-8 encoded filename
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`,
			"subconv.yaml", url.PathEscape(filename)))
	w.Header().Set("Profile-Title", base64.StdEncoding.EncodeToString([]byte(profileName)))
	w.Header().Set("Profile-Update-Interval", fmt.Sprintf("%d", cfg.UpdateInterval))
	if ui := s.store.GetUserInfo(); ui != "" {
		w.Header().Set("Subscription-Userinfo", ui)
	}

	w.Write(data)
}

// handleStatus 服务状态
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	s.store.mu.RLock()
	status := map[string]any{
		"node_count":  len(s.store.proxies),
		"last_update": formatTime(s.store.lastUpdate),
		"next_update": formatTime(s.store.nextUpdate),
		"updating":    s.store.updating.Load(),
	}
	s.store.mu.RUnlock()
	writeJSON(w, http.StatusOK, status)
}

// handleNodes 节点列表
func (s *Server) handleNodes(w http.ResponseWriter, r *http.Request) {
	proxies := s.store.GetProxies()
	nodes := make([]map[string]any, 0, len(proxies))
	for _, p := range proxies {
		node := map[string]any{
			"name":   p.Name,
			"type":   p.Type,
			"server": p.Server,
			"port":   p.Port,
			"source": p.SourceName,
		}
		if p.Network != "" && p.Network != "tcp" {
			node["network"] = p.Network
		}
		if p.TLS {
			node["tls"] = true
		}
		if p.RealityPublicKey != "" {
			node["reality"] = true
		}
		nodes = append(nodes, node)
	}
	writeJSON(w, http.StatusOK, nodes)
}

// handleGetConfig 获取配置
func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	path := config.FilePath()
	cfg := config.Get()
	writeJSON(w, http.StatusOK, map[string]any{
		"path":   path,
		"config": cfg,
	})
}

// handleSaveConfig 保存配置
func (s *Server) handleSaveConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Subscriptions  []config.Subscription `json:"subscriptions"`
		UpdateInterval *int                  `json:"update_interval"`
		SubToken       *string               `json:"sub_token"`
		ConfigName     *string               `json:"config_name"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "无效请求"})
		return
	}
	if req.Subscriptions != nil {
		config.UpdateSubscriptions(req.Subscriptions)
	}
	if req.ConfigName != nil {
		config.UpdateConfigName(*req.ConfigName)
	}
	if req.UpdateInterval != nil {
		config.UpdateField(func(c *config.Config) { c.UpdateInterval = *req.UpdateInterval })
	}
	if req.SubToken != nil {
		config.UpdateField(func(c *config.Config) { c.SubToken = *req.SubToken })
	}
	if err := config.Save(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleAddSub 添加订阅
func (s *Server) handleAddSub(w http.ResponseWriter, r *http.Request) {
	var sub config.Subscription
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&sub); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "无效请求"})
		return
	}
	sub.URL = strings.TrimSpace(sub.URL)
	sub.Name = strings.TrimSpace(sub.Name)
	if sub.URL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "URL不能为空"})
		return
	}
	if sub.Name == "" {
		sub.Name = "订阅" + fmt.Sprintf("%d", time.Now().Unix()%10000)
	}

	cfg := config.Get()
	// 检查重复
	for _, existing := range cfg.Subscriptions {
		if existing.URL == sub.URL {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "订阅已存在"})
			return
		}
	}
	cfg.Subscriptions = append(cfg.Subscriptions, sub)
	config.UpdateSubscriptions(cfg.Subscriptions)
	if err := config.Save(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleDeleteSub 删除订阅
func (s *Server) handleDeleteSub(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "无效请求"})
		return
	}
	cfg := config.Get()
	newSubs := make([]config.Subscription, 0, len(cfg.Subscriptions))
	for _, s := range cfg.Subscriptions {
		if s.URL != req.URL {
			newSubs = append(newSubs, s)
		}
	}
	config.UpdateSubscriptions(newSubs)
	if err := config.Save(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleRefresh 手动刷新
func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if s.store.updating.Load() {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "正在更新中"})
		return
	}
	s.store.TriggerRefresh()
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleLogs 获取日志
func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	logs := s.store.GetLogs()
	writeJSON(w, http.StatusOK, logs)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format("2006-01-02 15:04:05")
}
