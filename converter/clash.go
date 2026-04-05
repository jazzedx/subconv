package converter

import (
	"bytes"
	"regexp"

	"gopkg.in/yaml.v3"
)

// quotedString 强制 yaml 序列化时加引号，防止纯数字字符串被当作整数
type quotedString string

func (q quotedString) MarshalYAML() (interface{}, error) {
	return &yaml.Node{
		Kind:  yaml.ScalarNode,
		Value: string(q),
		Style: yaml.DoubleQuotedStyle,
	}, nil
}

// ProxyToMap 将 Proxy 转换为 Clash 格式的 map
func ProxyToMap(p Proxy) map[string]any {
	m := map[string]any{
		"name":   p.Name,
		"type":   p.Type,
		"server": p.Server,
		"port":   p.Port,
		"udp":    true,
	}

	switch p.Type {
	case "vless":
		buildVLESS(m, p)
	case "vmess":
		buildVMess(m, p)
	case "ss":
		m["cipher"] = p.Cipher
		m["password"] = p.Password
	case "trojan":
		buildTrojan(m, p)
	case "hysteria2":
		buildHysteria2(m, p)
	}

	if len(p.ALPN) > 0 {
		m["alpn"] = p.ALPN
	}

	return m
}

func buildVLESS(m map[string]any, p Proxy) {
	m["uuid"] = p.UUID
	if p.Network != "" && p.Network != "tcp" {
		m["network"] = p.Network
	}
	if p.TLS {
		m["tls"] = true
	}
	if p.Flow != "" {
		m["flow"] = p.Flow
	}
	if p.SNI != "" {
		m["servername"] = p.SNI
	}
	if p.Fingerprint != "" {
		m["client-fingerprint"] = p.Fingerprint
	}
	if p.SkipCertVerify {
		m["skip-cert-verify"] = true
	}

	// Reality
	if p.RealityPublicKey != "" {
		opts := map[string]any{"public-key": p.RealityPublicKey}
		if p.RealityShortID != "" {
			opts["short-id"] = quotedString(p.RealityShortID)
		}
		m["reality-opts"] = opts
	}

	addTransportOpts(m, p)
}

func buildVMess(m map[string]any, p Proxy) {
	m["uuid"] = p.UUID
	m["alterId"] = p.AlterId
	m["cipher"] = p.Cipher
	if p.Network != "" && p.Network != "tcp" {
		m["network"] = p.Network
	}
	if p.TLS {
		m["tls"] = true
	}
	if p.SNI != "" {
		m["servername"] = p.SNI
	}
	if p.Fingerprint != "" {
		m["client-fingerprint"] = p.Fingerprint
	}
	if p.SkipCertVerify {
		m["skip-cert-verify"] = true
	}

	addTransportOpts(m, p)
}

func buildTrojan(m map[string]any, p Proxy) {
	m["password"] = p.Password
	if p.SNI != "" {
		m["sni"] = p.SNI
	}
	if p.SkipCertVerify {
		m["skip-cert-verify"] = true
	}
	if p.Fingerprint != "" {
		m["client-fingerprint"] = p.Fingerprint
	}
	if p.Network != "" && p.Network != "tcp" {
		m["network"] = p.Network
		addTransportOpts(m, p)
	}
}

func buildHysteria2(m map[string]any, p Proxy) {
	m["password"] = p.Password
	if p.SNI != "" {
		m["sni"] = p.SNI
	}
	if p.SkipCertVerify {
		m["skip-cert-verify"] = true
	}
	if p.Fingerprint != "" {
		m["client-fingerprint"] = p.Fingerprint
	}
}

func addTransportOpts(m map[string]any, p Proxy) {
	switch p.Network {
	case "ws":
		opts := map[string]any{}
		if p.WSPath != "" {
			opts["path"] = p.WSPath
		}
		if p.WSHost != "" {
			opts["headers"] = map[string]any{"Host": p.WSHost}
		}
		if len(opts) > 0 {
			m["ws-opts"] = opts
		}
	case "grpc":
		if p.GRPCServiceName != "" {
			m["grpc-opts"] = map[string]any{
				"grpc-service-name": p.GRPCServiceName,
			}
		}
	case "h2":
		opts := map[string]any{}
		if p.H2Path != "" {
			opts["path"] = p.H2Path
		}
		if len(p.H2Host) > 0 {
			opts["host"] = p.H2Host
		}
		if len(opts) > 0 {
			m["h2-opts"] = opts
		}
	case "httpupgrade":
		opts := map[string]any{}
		if p.WSPath != "" {
			opts["path"] = p.WSPath
		}
		if p.WSHost != "" {
			opts["host"] = p.WSHost
		}
		if len(opts) > 0 {
			m["httpupgrade-opts"] = opts
		}
	}
}

// GenerateClashConfig 生成完整的 Clash Verge 配置 YAML
func GenerateClashConfig(proxies []Proxy) ([]byte, error) {
	proxyMaps := make([]map[string]any, 0, len(proxies))
	names := make([]string, 0, len(proxies))
	for _, p := range proxies {
		proxyMaps = append(proxyMaps, ProxyToMap(p))
		names = append(names, p.Name)
	}

	// 🔄 手动切换: DIRECT + 所有节点
	switchProxies := make([]string, 0, len(names)+1)
	switchProxies = append(switchProxies, "DIRECT")
	switchProxies = append(switchProxies, names...)

	// 服务分组引用主切换组 + 所有节点
	svc := func(defaultProxy string) []string {
		res := make([]string, 0, len(names)+2)
		if defaultProxy == "DIRECT" {
			res = append(res, "DIRECT", "🔄 手动切换")
		} else {
			res = append(res, "🔄 手动切换", "DIRECT")
		}
		res = append(res, names...)
		return res
	}

	cfg := map[string]any{
		"mixed-port":          7890,
		"allow-lan":           false,
		"mode":                "rule",
		"log-level":           "info",
		"unified-delay":       true,
		"tcp-concurrent":      true,
		"find-process-mode":   "strict",
		"external-controller": "127.0.0.1:9090",
		"dns": map[string]any{
			"enable":        true,
			"ipv6":          false,
			"enhanced-mode": "fake-ip",
			"fake-ip-range": "198.18.0.1/16",
			"default-nameserver": []string{
				"8.8.8.8",
				"1.1.1.1",
			},
			"proxy-server-nameserver": []string{
				"https://1.1.1.1/dns-query",
				"https://8.8.8.8/dns-query",
			},
			"nameserver": []string{
				"https://doh.pub/dns-query",
				"https://1.0.0.1/dns-query",
			},
			"fallback": []string{
				"https://1.1.1.1/dns-query",
				"https://8.8.8.8/dns-query",
			},
			"fallback-filter": map[string]any{
				"geoip":      true,
				"geoip-code": "CN",
			},
			"fake-ip-filter": []string{
				"*.lan",
				"localhost",
				"*.local",
			},
		},
		"proxies": proxyMaps,
		"proxy-groups": []map[string]any{
			{"name": "🔄 手动切换", "type": "select", "proxies": switchProxies},
			{"name": "🧲 OpenAI", "type": "select", "proxies": svc("proxy")},
			{"name": "🧲 Claude", "type": "select", "proxies": svc("proxy")},
			{"name": "🔎 Google", "type": "select", "proxies": svc("proxy")},
			{"name": "📲 聊天软件", "type": "select", "proxies": svc("proxy")},
			{"name": "🎙 Discord", "type": "select", "proxies": svc("proxy")},
			{"name": "📲 Instagram", "type": "select", "proxies": svc("proxy")},
			{"name": "🎬 YouTube", "type": "select", "proxies": svc("proxy")},
			{"name": "🎬 Netflix", "type": "select", "proxies": svc("proxy")},
			{"name": "🎶 TikTok", "type": "select", "proxies": svc("proxy")},
			{"name": "🌏 国外流媒体", "type": "select", "proxies": svc("proxy")},
			{"name": "🧩 微软服务", "type": "select", "proxies": svc("DIRECT")},
			{"name": "🍎 苹果服务", "type": "select", "proxies": svc("DIRECT")},
			{"name": "🎮 游戏平台", "type": "select", "proxies": svc("proxy")},
			{"name": "🌏 国外网站", "type": "select", "proxies": svc("proxy")},
			{"name": "🌏 国内网站", "type": "select", "proxies": svc("DIRECT")},
			{"name": "🐟 漏网之鱼", "type": "select", "proxies": svc("proxy")},
		},
		"rule-providers": buildRuleProviders(),
		"rules":          buildRules(),
	}

	return fixYAML(yaml.Marshal(cfg))
}

func buildRuleProviders() map[string]any {
	base := "https://cdn.jsdelivr.net/gh/Loyalsoldier/clash-rules@release/"
	domain := func(name string) map[string]any {
		return map[string]any{
			"type":     "http",
			"behavior": "domain",
			"url":      base + name + ".txt",
			"path":     "./ruleset/" + name + ".yaml",
			"interval": 86400,
		}
	}
	ipcidr := func(name string) map[string]any {
		return map[string]any{
			"type":     "http",
			"behavior": "ipcidr",
			"url":      base + name + ".txt",
			"path":     "./ruleset/" + name + ".yaml",
			"interval": 86400,
		}
	}
	return map[string]any{
		"reject":       domain("reject"),
		"icloud":       domain("icloud"),
		"apple":        domain("apple"),
		"google":       domain("google"),
		"proxy":        domain("proxy"),
		"direct":       domain("direct"),
		"private":      domain("private"),
		"gfw":          domain("gfw"),
		"tld-not-cn":   domain("tld-not-cn"),
		"telegramcidr": ipcidr("telegramcidr"),
		"cncidr":       ipcidr("cncidr"),
		"lancidr":      ipcidr("lancidr"),
		"applications": map[string]any{
			"type":     "http",
			"behavior": "classical",
			"url":      base + "applications.txt",
			"path":     "./ruleset/applications.yaml",
			"interval": 86400,
		},
	}
}

func buildRules() []string {
	return []string{
		// 拦截 QUIC，强制降级为 TCP 以支持域名匹配
		"AND,((NETWORK,UDP),(DST-PORT,443)),REJECT",

		// 直连应用
		"RULE-SET,applications,DIRECT",
		"RULE-SET,private,DIRECT",

		// AI 服务
		"DOMAIN-SUFFIX,openai.com,🧲 OpenAI",
		"DOMAIN-SUFFIX,chatgpt.com,🧲 OpenAI",
		"DOMAIN-SUFFIX,oaiusercontent.com,🧲 OpenAI",
		"DOMAIN-SUFFIX,oaistatic.com,🧲 OpenAI",
		"DOMAIN-SUFFIX,auth.openai.com,🧲 OpenAI",
		"DOMAIN,chatgpt.livekit.cloud,🧲 OpenAI",
		"DOMAIN-KEYWORD,openai,🧲 OpenAI",
		"DOMAIN-SUFFIX,anthropic.com,🧲 Claude",
		"DOMAIN-SUFFIX,claude.ai,🧲 Claude",
		"DOMAIN-SUFFIX,claudeusercontent.com,🧲 Claude",
		"DOMAIN-KEYWORD,claude,🧲 Claude",
		"DOMAIN-KEYWORD,anthropic,🧲 Claude",

		// 聊天软件（Telegram + WhatsApp + Line）
		"DOMAIN-SUFFIX,telegram.org,📲 聊天软件",
		"DOMAIN-SUFFIX,telegram.me,📲 聊天软件",
		"DOMAIN-SUFFIX,telegram-cdn.org,📲 聊天软件",
		"DOMAIN-SUFFIX,telegra.ph,📲 聊天软件",
		"DOMAIN-SUFFIX,t.me,📲 聊天软件",
		"DOMAIN-SUFFIX,tg.dev,📲 聊天软件",
		"DOMAIN-KEYWORD,telegram,📲 聊天软件",
		"RULE-SET,telegramcidr,📲 聊天软件",
		"DOMAIN-SUFFIX,whatsapp.com,📲 聊天软件",
		"DOMAIN-SUFFIX,whatsapp.net,📲 聊天软件",
		"DOMAIN-KEYWORD,whatsapp,📲 聊天软件",
		"DOMAIN-SUFFIX,line.me,📲 聊天软件",
		"DOMAIN-SUFFIX,line-cdn.net,📲 聊天软件",
		"DOMAIN-SUFFIX,line-scdn.net,📲 聊天软件",
		"DOMAIN-SUFFIX,line-apps.com,📲 聊天软件",
		"DOMAIN-SUFFIX,naver.jp,📲 聊天软件",

		// Discord
		"DOMAIN-SUFFIX,discord.com,🎙 Discord",
		"DOMAIN-SUFFIX,discord.gg,🎙 Discord",
		"DOMAIN-SUFFIX,discordapp.com,🎙 Discord",
		"DOMAIN-SUFFIX,discordapp.net,🎙 Discord",
		"DOMAIN-SUFFIX,discordcdn.com,🎙 Discord",
		"DOMAIN-SUFFIX,discord.media,🎙 Discord",

		// Instagram
		"DOMAIN-SUFFIX,instagram.com,📲 Instagram",
		"DOMAIN-SUFFIX,cdninstagram.com,📲 Instagram",
		"DOMAIN-SUFFIX,ig.me,📲 Instagram",
		"DOMAIN-SUFFIX,igcdn.com,📲 Instagram",
		"DOMAIN-SUFFIX,igsonar.com,📲 Instagram",
		"DOMAIN-KEYWORD,instagram,📲 Instagram",

		// YouTube
		"DOMAIN-SUFFIX,youtube.com,🎬 YouTube",
		"DOMAIN-SUFFIX,youtu.be,🎬 YouTube",
		"DOMAIN-SUFFIX,ytimg.com,🎬 YouTube",
		"DOMAIN-SUFFIX,ggpht.com,🎬 YouTube",
		"DOMAIN-SUFFIX,googlevideo.com,🎬 YouTube",
		"DOMAIN-SUFFIX,youtube-nocookie.com,🎬 YouTube",

		// Netflix
		"DOMAIN-SUFFIX,netflix.com,🎬 Netflix",
		"DOMAIN-SUFFIX,nflximg.net,🎬 Netflix",
		"DOMAIN-SUFFIX,nflximg.com,🎬 Netflix",
		"DOMAIN-SUFFIX,nflxvideo.net,🎬 Netflix",
		"DOMAIN-SUFFIX,nflxso.net,🎬 Netflix",
		"DOMAIN-SUFFIX,nflxext.com,🎬 Netflix",

		// TikTok
		"DOMAIN-SUFFIX,tiktok.com,🎶 TikTok",
		"DOMAIN-SUFFIX,tiktokv.com,🎶 TikTok",
		"DOMAIN-SUFFIX,tiktokcdn.com,🎶 TikTok",
		"DOMAIN-KEYWORD,tiktok,🎶 TikTok",

		// 国外流媒体
		"DOMAIN-SUFFIX,spotify.com,🌏 国外流媒体",
		"DOMAIN-SUFFIX,scdn.co,🌏 国外流媒体",
		"DOMAIN-SUFFIX,disneyplus.com,🌏 国外流媒体",
		"DOMAIN-SUFFIX,disney-plus.net,🌏 国外流媒体",
		"DOMAIN-SUFFIX,hbo.com,🌏 国外流媒体",
		"DOMAIN-SUFFIX,hbomax.com,🌏 国外流媒体",
		"DOMAIN-SUFFIX,primevideo.com,🌏 国外流媒体",
		"DOMAIN-SUFFIX,amazon.com,🌏 国外流媒体",
		"DOMAIN-SUFFIX,twitch.tv,🌏 国外流媒体",

		// Google
		"RULE-SET,google,🔎 Google",

		// 微软服务
		"DOMAIN-SUFFIX,microsoft.com,🧩 微软服务",
		"DOMAIN-SUFFIX,windows.com,🧩 微软服务",
		"DOMAIN-SUFFIX,windows.net,🧩 微软服务",
		"DOMAIN-SUFFIX,microsoftonline.com,🧩 微软服务",
		"DOMAIN-SUFFIX,office.com,🧩 微软服务",
		"DOMAIN-SUFFIX,office365.com,🧩 微软服务",
		"DOMAIN-SUFFIX,live.com,🧩 微软服务",
		"DOMAIN-SUFFIX,msn.com,🧩 微软服务",
		"DOMAIN-SUFFIX,onedrive.com,🧩 微软服务",
		"DOMAIN-SUFFIX,sharepoint.com,🧩 微软服务",

		// 苹果服务
		"RULE-SET,apple,🍎 苹果服务",
		"RULE-SET,icloud,🍎 苹果服务",

		// 游戏平台
		"DOMAIN-SUFFIX,steampowered.com,🎮 游戏平台",
		"DOMAIN-SUFFIX,steamcommunity.com,🎮 游戏平台",
		"DOMAIN-SUFFIX,steamstatic.com,🎮 游戏平台",
		"DOMAIN-SUFFIX,epicgames.com,🎮 游戏平台",
		"DOMAIN-SUFFIX,unrealengine.com,🎮 游戏平台",
		"DOMAIN-SUFFIX,battle.net,🎮 游戏平台",
		"DOMAIN-SUFFIX,blizzard.com,🎮 游戏平台",
		"DOMAIN-SUFFIX,ea.com,🎮 游戏平台",
		"DOMAIN-SUFFIX,ubisoft.com,🎮 游戏平台",

		// 广告拦截
		"RULE-SET,reject,REJECT",

		// 基础路由（rule-providers）
		"RULE-SET,direct,🌏 国内网站",
		"RULE-SET,proxy,🌏 国外网站",
		"RULE-SET,gfw,🌏 国外网站",
		"RULE-SET,tld-not-cn,🌏 国外网站",
		"RULE-SET,lancidr,DIRECT,no-resolve",
		"RULE-SET,cncidr,🌏 国内网站,no-resolve",

		// 兜底
		"GEOIP,LAN,DIRECT,no-resolve",
		"GEOIP,CN,🌏 国内网站,no-resolve",
		"MATCH,🐟 漏网之鱼",
	}
}

// GenerateProxiesYAML 只生成 proxies 部分
func GenerateProxiesYAML(proxies []Proxy) ([]byte, error) {
	proxyMaps := make([]map[string]any, 0, len(proxies))
	for _, p := range proxies {
		proxyMaps = append(proxyMaps, ProxyToMap(p))
	}
	return fixYAML(yaml.Marshal(map[string]any{"proxies": proxyMaps}))
}

// fixYAML 修复 yaml.v3 对 Unicode 字符的转义问题
func fixYAML(data []byte, err error) ([]byte, error) {
	if err != nil {
		return nil, err
	}
	// yaml.v3 会把含 emoji 的字符串用双引号包裹并转义为 \UXXXXXXXX
	// 替换为不带引号的原始 UTF-8
	re := regexp.MustCompile(`"((?:[^"\\]|\\[^U]|\\U[0-9a-fA-F]{8})*)"`)
	data = re.ReplaceAllFunc(data, func(match []byte) []byte {
		inner := match[1 : len(match)-1]
		// 只处理含有 \U 转义的
		if !bytes.Contains(inner, []byte(`\U`)) {
			return match
		}
		// 用 yaml.Unmarshal 来正确解码
		var s string
		if err := yaml.Unmarshal(match, &s); err == nil {
			return []byte(s)
		}
		return match
	})
	return data, nil
}
