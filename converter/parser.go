package converter

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Proxy 代理节点
type Proxy struct {
	Name   string
	Type   string // vless, vmess, ss, trojan, hysteria2
	Server string
	Port   int

	// Auth
	UUID     string
	AlterId  int
	Cipher   string
	Password string

	// Transport
	Network string // tcp, ws, grpc, h2, httpupgrade

	// TLS
	TLS            bool
	SNI            string
	ALPN           []string
	SkipCertVerify bool
	Fingerprint    string

	// Reality
	RealityPublicKey string
	RealityShortID   string

	// Flow
	Flow string

	// WebSocket
	WSPath string
	WSHost string

	// gRPC
	GRPCServiceName string

	// H2
	H2Path string
	H2Host []string

	// Source
	SourceName string
}

// ParseContent 解析订阅内容，返回代理列表
func ParseContent(raw []byte, sourceName string) []Proxy {
	content := strings.TrimSpace(string(raw))
	if content == "" {
		return nil
	}

	// 尝试 base64 解码
	decoded := tryBase64Decode(content)
	if decoded != "" {
		content = decoded
	}

	var proxies []Proxy
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		p, err := parseLine(line)
		if err != nil {
			continue
		}
		p.SourceName = sourceName
		proxies = append(proxies, *p)
	}
	return proxies
}

func tryBase64Decode(s string) string {
	// 如果包含协议头，说明已经是明文
	if strings.Contains(s, "://") {
		return ""
	}
	// 尝试标准 base64
	s = strings.TrimSpace(s)
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding,
		base64.URLEncoding,
		base64.RawStdEncoding,
		base64.RawURLEncoding,
	} {
		if decoded, err := enc.DecodeString(s); err == nil {
			result := string(decoded)
			if strings.Contains(result, "://") {
				return result
			}
		}
	}
	return ""
}

func parseLine(line string) (*Proxy, error) {
	idx := strings.Index(line, "://")
	if idx < 0 {
		return nil, fmt.Errorf("无效协议格式")
	}
	scheme := strings.ToLower(line[:idx])
	switch scheme {
	case "vless":
		return parseVLESS(line)
	case "vmess":
		return parseVMess(line)
	case "ss":
		return parseSS(line)
	case "trojan":
		return parseTrojan(line)
	case "hysteria2", "hy2":
		return parseHysteria2(line)
	default:
		return nil, fmt.Errorf("不支持的协议: %s", scheme)
	}
}

// parseVLESS 解析 vless://uuid@server:port?params#name
func parseVLESS(raw string) (*Proxy, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}

	port, _ := strconv.Atoi(u.Port())
	if port == 0 {
		return nil, fmt.Errorf("无效端口")
	}

	name := u.Fragment
	if name == "" {
		name = fmt.Sprintf("%s:%d", u.Hostname(), port)
	}
	name, _ = url.PathUnescape(name)

	q := u.Query()
	p := &Proxy{
		Name:    name,
		Type:    "vless",
		Server:  u.Hostname(),
		Port:    port,
		UUID:    u.User.Username(),
		Network: q.Get("type"),
		Flow:    q.Get("flow"),
	}

	if p.Network == "" {
		p.Network = "tcp"
	}

	security := q.Get("security")
	switch security {
	case "tls":
		p.TLS = true
		p.SNI = q.Get("sni")
		p.Fingerprint = q.Get("fp")
		if alpn := q.Get("alpn"); alpn != "" {
			p.ALPN = strings.Split(alpn, ",")
		}
	case "reality":
		p.TLS = true
		p.SNI = q.Get("sni")
		p.Fingerprint = q.Get("fp")
		p.RealityPublicKey = q.Get("pbk")
		p.RealityShortID = q.Get("sid")
	}

	switch p.Network {
	case "ws":
		p.WSPath = q.Get("path")
		p.WSHost = q.Get("host")
		if p.WSPath == "" {
			p.WSPath = "/"
		}
	case "grpc":
		p.GRPCServiceName = q.Get("serviceName")
	case "h2":
		p.H2Path = q.Get("path")
		if host := q.Get("host"); host != "" {
			p.H2Host = strings.Split(host, ",")
		}
	case "httpupgrade":
		p.WSPath = q.Get("path")
		p.WSHost = q.Get("host")
	}

	return p, nil
}

// vmessJSON VMess 链接的JSON结构
type vmessJSON struct {
	V    any    `json:"v"`
	Ps   string `json:"ps"`
	Add  string `json:"add"`
	Port any    `json:"port"`
	ID   string `json:"id"`
	Aid  any    `json:"aid"`
	Scy  string `json:"scy"`
	Net  string `json:"net"`
	Type string `json:"type"`
	Host string `json:"host"`
	Path string `json:"path"`
	TLS  string `json:"tls"`
	SNI  string `json:"sni"`
	ALPN string `json:"alpn"`
	Fp   string `json:"fp"`
}

// parseVMess 解析 vmess://base64json
func parseVMess(raw string) (*Proxy, error) {
	encoded := raw[len("vmess://"):]
	// 补全 base64 padding
	if m := len(encoded) % 4; m != 0 {
		encoded += strings.Repeat("=", 4-m)
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		decoded, err = base64.URLEncoding.DecodeString(encoded)
		if err != nil {
			return nil, fmt.Errorf("vmess base64 解码失败: %w", err)
		}
	}

	var v vmessJSON
	if err := json.Unmarshal(decoded, &v); err != nil {
		return nil, fmt.Errorf("vmess JSON 解析失败: %w", err)
	}

	port := toInt(v.Port)
	if port == 0 {
		return nil, fmt.Errorf("无效端口")
	}
	alterId := toInt(v.Aid)

	name := v.Ps
	if name == "" {
		name = fmt.Sprintf("%s:%d", v.Add, port)
	}

	cipher := v.Scy
	if cipher == "" {
		cipher = "auto"
	}

	p := &Proxy{
		Name:        name,
		Type:        "vmess",
		Server:      v.Add,
		Port:        port,
		UUID:        v.ID,
		AlterId:     alterId,
		Cipher:      cipher,
		Network:     v.Net,
		Fingerprint: v.Fp,
	}

	if p.Network == "" {
		p.Network = "tcp"
	}

	if v.TLS == "tls" {
		p.TLS = true
		p.SNI = v.SNI
		if v.ALPN != "" {
			p.ALPN = strings.Split(v.ALPN, ",")
		}
	}

	switch p.Network {
	case "ws":
		p.WSPath = v.Path
		p.WSHost = v.Host
		if p.WSPath == "" {
			p.WSPath = "/"
		}
	case "grpc":
		p.GRPCServiceName = v.Path
	case "h2":
		p.H2Path = v.Path
		if v.Host != "" {
			p.H2Host = strings.Split(v.Host, ",")
		}
	}

	return p, nil
}

// parseSS 解析 ss://base64(method:password)@server:port#name
func parseSS(raw string) (*Proxy, error) {
	// 去掉 ss://
	raw = raw[len("ss://"):]

	// 提取 fragment (节点名)
	var name string
	if idx := strings.LastIndex(raw, "#"); idx >= 0 {
		name, _ = url.PathUnescape(raw[idx+1:])
		raw = raw[:idx]
	}

	var method, password, server string
	var port int

	if atIdx := strings.LastIndex(raw, "@"); atIdx >= 0 {
		// SIP002 格式: base64(method:password)@server:port
		userInfo := raw[:atIdx]
		hostPort := raw[atIdx+1:]

		// 去掉查询参数
		if qIdx := strings.Index(hostPort, "?"); qIdx >= 0 {
			hostPort = hostPort[:qIdx]
		}

		decoded, err := base64Decode(userInfo)
		if err != nil {
			return nil, fmt.Errorf("ss userinfo 解码失败: %w", err)
		}

		parts := strings.SplitN(decoded, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("ss userinfo 格式错误")
		}
		method = parts[0]
		password = parts[1]

		h, p, err := parseHostPort(hostPort)
		if err != nil {
			return nil, err
		}
		server = h
		port = p
	} else {
		// 旧格式: base64(method:password@server:port)
		decoded, err := base64Decode(raw)
		if err != nil {
			return nil, fmt.Errorf("ss 解码失败: %w", err)
		}
		parts := strings.SplitN(decoded, "@", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("ss 格式错误")
		}
		mp := strings.SplitN(parts[0], ":", 2)
		if len(mp) != 2 {
			return nil, fmt.Errorf("ss method:password 格式错误")
		}
		method = mp[0]
		password = mp[1]
		h, p, err := parseHostPort(parts[1])
		if err != nil {
			return nil, err
		}
		server = h
		port = p
	}

	if name == "" {
		name = fmt.Sprintf("%s:%d", server, port)
	}

	return &Proxy{
		Name:     name,
		Type:     "ss",
		Server:   server,
		Port:     port,
		Cipher:   method,
		Password: password,
	}, nil
}

// parseTrojan 解析 trojan://password@server:port?params#name
func parseTrojan(raw string) (*Proxy, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}

	port, _ := strconv.Atoi(u.Port())
	if port == 0 {
		return nil, fmt.Errorf("无效端口")
	}

	name := u.Fragment
	if name == "" {
		name = fmt.Sprintf("%s:%d", u.Hostname(), port)
	}
	name, _ = url.PathUnescape(name)

	q := u.Query()
	p := &Proxy{
		Name:     name,
		Type:     "trojan",
		Server:   u.Hostname(),
		Port:     port,
		Password: u.User.Username(),
		TLS:      true,
		SNI:      q.Get("sni"),
		Network:  q.Get("type"),
	}

	if p.SNI == "" {
		p.SNI = u.Hostname()
	}
	if p.Network == "" {
		p.Network = "tcp"
	}

	if q.Get("allowInsecure") == "1" {
		p.SkipCertVerify = true
	}
	if fp := q.Get("fp"); fp != "" {
		p.Fingerprint = fp
	}
	if alpn := q.Get("alpn"); alpn != "" {
		p.ALPN = strings.Split(alpn, ",")
	}

	switch p.Network {
	case "ws":
		p.WSPath = q.Get("path")
		p.WSHost = q.Get("host")
	case "grpc":
		p.GRPCServiceName = q.Get("serviceName")
	}

	return p, nil
}

// parseHysteria2 解析 hysteria2://password@server:port?params#name
func parseHysteria2(raw string) (*Proxy, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}

	port, _ := strconv.Atoi(u.Port())
	if port == 0 {
		return nil, fmt.Errorf("无效端口")
	}

	name := u.Fragment
	if name == "" {
		name = fmt.Sprintf("%s:%d", u.Hostname(), port)
	}
	name, _ = url.PathUnescape(name)

	q := u.Query()
	p := &Proxy{
		Name:     name,
		Type:     "hysteria2",
		Server:   u.Hostname(),
		Port:     port,
		Password: u.User.Username(),
		SNI:      q.Get("sni"),
	}

	if q.Get("insecure") == "1" {
		p.SkipCertVerify = true
	}
	if p.SNI == "" {
		p.SNI = u.Hostname()
	}

	return p, nil
}

// --- helpers ---

func toInt(v any) int {
	switch val := v.(type) {
	case float64:
		return int(val)
	case int:
		return val
	case string:
		n, _ := strconv.Atoi(val)
		return n
	case json.Number:
		n, _ := val.Int64()
		return int(n)
	default:
		return 0
	}
}

func base64Decode(s string) (string, error) {
	s = strings.TrimSpace(s)
	if m := len(s) % 4; m != 0 {
		s += strings.Repeat("=", 4-m)
	}
	decoded, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		decoded, err = base64.URLEncoding.DecodeString(s)
	}
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(strings.TrimRight(s, "="))
	}
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

func parseHostPort(s string) (string, int, error) {
	// 处理 IPv6: [::1]:port
	if strings.HasPrefix(s, "[") {
		end := strings.Index(s, "]")
		if end < 0 {
			return "", 0, fmt.Errorf("无效的IPv6地址")
		}
		host := s[1:end]
		portStr := ""
		if end+1 < len(s) && s[end+1] == ':' {
			portStr = s[end+2:]
		}
		port, _ := strconv.Atoi(portStr)
		return host, port, nil
	}

	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return "", 0, fmt.Errorf("无效的 host:port 格式")
	}
	port, _ := strconv.Atoi(parts[1])
	return parts[0], port, nil
}
