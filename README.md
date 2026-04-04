# SubConv

轻量级订阅转换服务 —— 将 3X-UI 订阅转换为 Clash Verge 配置，单文件部署，开箱即用。

## 特性

- 支持 VLESS (Reality)、VMess、Shadowsocks、Trojan、Hysteria2
- 支持 TCP、WebSocket、gRPC、H2、HTTPUpgrade 传输
- 内置 Web 管理面板，订阅/节点/日志一目了然
- Clash Verge 拉取时自动触发上游刷新，始终返回最新节点
- 透传上游 Subscription-Userinfo（流量/到期时间）
- 内置 16 个代理组 + Loyalsoldier 规则集，覆盖常见场景
- 自动按 server:port:protocol 去重
- 单二进制，零外部依赖，支持 systemd 管理

## 快速开始

### 编译

```bash
# Go 1.22+
make build          # 当前平台
make linux          # Linux amd64
make linux-arm64    # Linux arm64
make all            # 全平台
```

### 运行

```bash
./subconv                        # 首次运行自动生成 config.yaml
./subconv -c /path/to/config.yaml  # 指定配置文件
```

首次启动会自动生成 API 密钥，查看配置文件获取。

### 使用

1. 浏览器访问 `http://IP:8866/admin`，输入 API 密钥登录
2. 添加 3X-UI 订阅链接，点击「全部刷新」
3. 复制页面顶部的订阅地址，粘贴到 Clash Verge

### 一键安装（Linux）

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/jazzedx/subconv/main/install.sh)

# 国内加速
bash <(curl -fsSL https://ghfast.top/https://raw.githubusercontent.com/jazzedx/subconv/main/install.sh) https://ghfast.top/
```

安装后使用 `subconv` 命令管理：

```bash
subconv start       # 启动
subconv stop        # 停止
subconv restart     # 重启
subconv status      # 查看状态
subconv log         # 查看日志
subconv config      # 编辑配置文件
subconv tls         # 配置 TLS 证书
subconv uninstall   # 卸载
```

也可以直接运行脚本打开交互式菜单，或使用 systemd 命令管理。

### Docker

```bash
docker run -d \
  --name subconv \
  -p 8866:8866 \
  -v ./config.yaml:/app/config.yaml \
  subconv
```

## 订阅地址

```
http://IP:8866/sub
http://IP:8866/sub?token=xxx          # 带令牌
http://IP:8866/sub?name=自定义名称     # 自定义配置名
```

配置 TLS 后使用 `https://` 访问。

Clash Verge 每次拉取都会触发上游同步刷新，确保节点最新。

响应头包含 `Content-Disposition`（RFC 5987）、`Profile-Title`、`Profile-Update-Interval`、`Subscription-Userinfo`。

## 配置文件

首次运行自动生成 `config.yaml`：

```yaml
listen: ":8866"             # 监听地址
# tls-cert: "/path/to/fullchain.pem"  # TLS 证书（留空则使用 HTTP）
# tls-key: "/path/to/privkey.pem"     # TLS 私钥
api-key: ""                 # 管理面板密钥（留空自动生成）
sub-token: ""               # 订阅访问令牌（留空则不鉴权）
config-name: "SubConv"      # Clash Verge 显示的配置名称
update-interval: 30         # 自动更新间隔（分钟），0 为关闭
timeout: 15                 # 拉取订阅超时（秒）
user-agent: "clash-verge/v2.0"

subscriptions:
  - url: "https://your-server/sub/xxx"
    name: "我的VPS"
```

## 代理组

| 代理组 | 类型 | 默认 |
|--------|------|------|
| 🔄 手动切换 | select | DIRECT + 所有节点 |
| 🧲 OpenAI | select | 🔄 手动切换 |
| 🧲 Claude | select | 🔄 手动切换 |
| 🔎 Google | select | 🔄 手动切换 |
| 📲 聊天软件 | select | 🔄 手动切换 |
| 🎙 Discord | select | 🔄 手动切换 |
| 📲 Instagram | select | 🔄 手动切换 |
| 🎬 YouTube | select | 🔄 手动切换 |
| 🎬 Netflix | select | 🔄 手动切换 |
| 🎶 TikTok | select | 🔄 手动切换 |
| 🌏 国外流媒体 | select | 🔄 手动切换 |
| 🧩 微软服务 | select | DIRECT |
| 🍎 苹果服务 | select | DIRECT |
| 🎮 游戏平台 | select | 🔄 手动切换 |
| 🌏 国外网站 | select | 🔄 手动切换 |
| 🌏 国内网站 | select | DIRECT |
| 🐟 漏网之鱼 | select | 🔄 手动切换 |

每个服务组均包含 🔄 手动切换 + DIRECT + 全部节点，可独立选择出口。

## 规则

基于 [Loyalsoldier/clash-rules](https://github.com/Loyalsoldier/clash-rules) 规则集（每日自动更新）：

- 13 个 rule-provider：reject / private / direct / proxy / gfw / tld-not-cn / google / apple / icloud / telegramcidr / cncidr / lancidr / applications
- 精细域名规则：OpenAI、Claude、Telegram、WhatsApp、Line、Discord、Instagram、YouTube、Netflix、TikTok、Spotify、Disney+、Microsoft、Steam 等
- 白名单模式：未匹配流量走 🐟 漏网之鱼

## API

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/sub` | Clash 订阅（sub-token 鉴权） |
| GET | `/admin` | 管理面板 |
| GET | `/api/status` | 服务状态 |
| GET | `/api/nodes` | 节点列表 |
| GET | `/api/config` | 获取配置 |
| POST | `/api/config` | 保存配置 |
| POST | `/api/subscriptions` | 添加订阅 |
| POST | `/api/subscriptions/delete` | 删除订阅 |
| POST | `/api/refresh` | 手动刷新 |
| GET | `/api/logs` | 运行日志 |

API 鉴权：`X-API-Key` 请求头或 `?key=xxx` 查询参数。

## License

MIT
