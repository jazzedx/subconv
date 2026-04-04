package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v3"
)

type Subscription struct {
	URL  string `yaml:"url"  json:"url"`
	Name string `yaml:"name" json:"name"`
}

type Config struct {
	Listen         string         `yaml:"listen"          json:"listen"`
	APIKey         string         `yaml:"api-key"         json:"api-key"`
	SubToken       string         `yaml:"sub-token"       json:"sub-token"`
	ConfigName     string         `yaml:"config-name"     json:"config-name"`
	UpdateInterval int            `yaml:"update-interval" json:"update-interval"`
	Timeout        int            `yaml:"timeout"         json:"timeout"`
	UserAgent      string         `yaml:"user-agent"      json:"user-agent"`
	Subscriptions  []Subscription `yaml:"subscriptions"   json:"subscriptions"`
}

var (
	Global   Config
	mu       sync.RWMutex
	filePath string
)

const defaultConfig = `# 监听地址
listen: ":8866"

# Web管理面板API密钥（留空则自动生成）
api-key: ""

# 订阅访问令牌（留空则不需要令牌，设置后访问 /sub?token=xxx）
sub-token: ""

# 配置名称（Clash Verge 显示的配置文件名，支持中文）
config-name: "SubConv"

# 自动更新间隔（分钟），0 表示不自动更新
update-interval: 30

# 拉取订阅超时时间（秒）
timeout: 15

# 拉取订阅时使用的 User-Agent
user-agent: "clash-verge/v2.0"

# 订阅链接列表
subscriptions:
  # - url: "https://your-3xui-server/sub/xxx"
  #   name: "我的VPS"
`

func Load(path string) error {
	mu.Lock()
	defer mu.Unlock()

	filePath = path

	if _, err := os.Stat(path); os.IsNotExist(err) {
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("创建目录失败: %w", err)
		}
		if err := os.WriteFile(path, []byte(defaultConfig), 0600); err != nil {
			return fmt.Errorf("写入默认配置失败: %w", err)
		}
		slog.Info("已生成默认配置文件", "path", path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读取配置文件失败: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("解析配置文件失败: %w", err)
	}

	if cfg.Listen == "" {
		cfg.Listen = ":8866"
	}
	if cfg.UpdateInterval < 0 {
		cfg.UpdateInterval = 0
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 15
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = "clash-verge/v2.0"
	}
	if cfg.ConfigName == "" {
		cfg.ConfigName = "SubConv"
	}
	if cfg.APIKey == "" {
		cfg.APIKey = generateKey(16)
		slog.Warn("已自动生成API密钥，请查看配置文件", "path", path)
	}

	Global = cfg
	return nil
}

func Save() error {
	mu.Lock()
	data, err := yaml.Marshal(&Global)
	mu.Unlock()
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, data, 0600)
}

func Get() Config {
	mu.RLock()
	defer mu.RUnlock()
	c := Global
	subs := make([]Subscription, len(Global.Subscriptions))
	copy(subs, Global.Subscriptions)
	c.Subscriptions = subs
	return c
}

func UpdateSubscriptions(subs []Subscription) {
	mu.Lock()
	defer mu.Unlock()
	Global.Subscriptions = subs
}

func UpdateConfigName(name string) {
	mu.Lock()
	defer mu.Unlock()
	Global.ConfigName = name
}

func UpdateField(fn func(*Config)) {
	mu.Lock()
	defer mu.Unlock()
	fn(&Global)
}

func FilePath() string {
	mu.RLock()
	defer mu.RUnlock()
	return filePath
}

func generateKey(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}
