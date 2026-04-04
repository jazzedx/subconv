#!/bin/sh
# SubConv 管理脚本
# 安装: curl -fsSL https://raw.githubusercontent.com/jazzedx/subconv/main/install.sh | bash
# 加速: bash <(curl -fsSL https://ghfast.top/https://raw.githubusercontent.com/jazzedx/subconv/main/install.sh) https://ghfast.top/

set -e

# ============ 配置 ============
REPO="jazzedx/subconv"
INSTALL_DIR="/opt/subconv"
BINARY_NAME="subconv"
SERVICE_NAME="subconv"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"
CONFIG_FILE="${INSTALL_DIR}/config.yaml"
GITHUB_API="https://api.github.com/repos/${REPO}/releases/latest"
GITHUB_PROXY=""

# ============ 运行状态 ============
HAS_SYSTEMD=1

# ============ 颜色输出 ============
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m'

info()  { printf "${BLUE}[INFO]${NC} %s\n" "$1"; }
ok()    { printf "${GREEN}[ OK ]${NC} %s\n" "$1"; }
warn()  { printf "${YELLOW}[WARN]${NC} %s\n" "$1"; }
error() { printf "${RED}[ERROR]${NC} %s\n" "$1"; exit 1; }

# ============ 前置检查 ============
check_root() {
    if [ "$(id -u)" -ne 0 ]; then
        error "请使用 root 用户或 sudo 运行此脚本"
    fi
}

check_os() {
    if [ "$(uname -s)" != "Linux" ]; then
        error "此脚本仅支持 Linux 系统"
    fi
}

check_systemd() {
    if ! command -v systemctl >/dev/null 2>&1; then
        HAS_SYSTEMD=0
    fi
}

check_download_tool() {
    if command -v curl >/dev/null 2>&1; then
        DOWNLOADER="curl"
    elif command -v wget >/dev/null 2>&1; then
        DOWNLOADER="wget"
    else
        error "需要 curl 或 wget，请先安装其中之一"
    fi
}

# ============ 下载封装 ============
download() {
    url="$1"; output="$2"
    if [ "$DOWNLOADER" = "curl" ]; then
        curl -fsSL -o "$output" "$url"
    else
        wget -qO "$output" "$url"
    fi
}

fetch_url() {
    url="$1"
    if [ "$DOWNLOADER" = "curl" ]; then
        curl -fsSL "$url"
    else
        wget -qO- "$url"
    fi
}

# ============ 架构检测 ============
detect_arch() {
    arch="$(uname -m)"
    case "$arch" in
        x86_64|amd64)   ARCH="amd64" ;;
        aarch64|arm64)   ARCH="arm64" ;;
        *)               error "不支持的架构: $arch" ;;
    esac
}

# ============ 状态检查 ============
is_installed() {
    [ -f "${INSTALL_DIR}/${BINARY_NAME}" ]
}

is_running() {
    if [ "$HAS_SYSTEMD" -eq 1 ]; then
        systemctl is-active --quiet "$SERVICE_NAME" 2>/dev/null
    else
        pgrep -x "$BINARY_NAME" >/dev/null 2>&1
    fi
}

get_current_version() {
    if is_installed; then
        "${INSTALL_DIR}/${BINARY_NAME}" --version 2>/dev/null || echo "未知"
    else
        echo "未安装"
    fi
}

# ============ 安装/升级 ============
do_install() {
    check_download_tool
    detect_arch

    IS_UPGRADE=0
    if is_installed; then
        IS_UPGRADE=1
        info "检测到已有安装，将执行升级操作"
    fi

    info "正在获取最新版本..."
    LATEST_VERSION=$(fetch_url "$GITHUB_API" | grep '"tag_name"' | head -1 | sed 's/.*"tag_name": *"//;s/".*//')
    if [ -z "$LATEST_VERSION" ]; then
        error "无法获取最新版本号，请检查网络连接"
    fi
    ok "最新版本: $LATEST_VERSION"

    FILE_NAME="${BINARY_NAME}-linux-${ARCH}"
    DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${LATEST_VERSION}/${FILE_NAME}"
    if [ -n "$GITHUB_PROXY" ]; then
        DOWNLOAD_URL="${GITHUB_PROXY}${DOWNLOAD_URL}"
    fi

    TMP_DIR=$(mktemp -d)
    trap 'rm -rf "$TMP_DIR"' EXIT

    info "正在下载 ${FILE_NAME}..."
    download "$DOWNLOAD_URL" "${TMP_DIR}/${BINARY_NAME}"
    ok "下载完成"

    # 升级时停止服务
    if [ "$IS_UPGRADE" -eq 1 ] && is_running; then
        warn "正在停止运行中的服务..."
        do_stop
    fi

    mkdir -p "$INSTALL_DIR"
    cp "${TMP_DIR}/${BINARY_NAME}" "${INSTALL_DIR}/${BINARY_NAME}"
    chmod +x "${INSTALL_DIR}/${BINARY_NAME}"

    if [ "$IS_UPGRADE" -eq 1 ]; then
        ok "升级完成"
    else
        ok "安装完成: ${INSTALL_DIR}/${BINARY_NAME}"
    fi

    # 配置 systemd
    if [ "$HAS_SYSTEMD" -eq 1 ]; then
        setup_systemd
        if [ "$IS_UPGRADE" -eq 0 ]; then
            systemctl enable "$SERVICE_NAME" 2>/dev/null
            ok "已设置开机自启动"
        fi
        do_start
    fi

    # 新安装时询问是否配置 TLS
    if [ "$IS_UPGRADE" -eq 0 ]; then
        printf "\n"
        printf "${YELLOW}是否配置 TLS 证书以启用 HTTPS？[y/N]: ${NC}"
        read -r answer < /dev/tty
        case "$answer" in
            [yY]|[yY][eE][sS]) do_tls ;;
        esac
    fi

    printf "\n"
    printf "${GREEN}========================================${NC}\n"
    if [ "$IS_UPGRADE" -eq 1 ]; then
        printf "${GREEN}  SubConv 升级成功！${NC}\n"
    else
        printf "${GREEN}  SubConv 安装成功！${NC}\n"
    fi
    printf "${GREEN}========================================${NC}\n"
    printf "  版本:     %s\n" "$LATEST_VERSION"
    printf "  目录:     %s\n" "$INSTALL_DIR"
    printf "  配置:     %s\n" "$CONFIG_FILE"
    printf "  管理:     subconv {start|stop|restart|status|log|config|tls|uninstall}\n"
    printf "\n"
}

# ============ 配置 systemd ============
setup_systemd() {
    cat > "$SERVICE_FILE" <<EOF
[Unit]
Description=SubConv - 订阅转换服务
After=network-online.target
Wants=network-online.target
StartLimitBurst=5
StartLimitIntervalSec=60

[Service]
Type=simple
WorkingDirectory=${INSTALL_DIR}
ExecStart=${INSTALL_DIR}/${BINARY_NAME}
Restart=on-failure
RestartSec=5
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF
    systemctl daemon-reload
}

# ============ 启动 ============
do_start() {
    if ! is_installed; then
        error "SubConv 未安装"
    fi
    if is_running; then
        warn "SubConv 已在运行中"
        return
    fi
    if [ "$HAS_SYSTEMD" -eq 1 ]; then
        systemctl start "$SERVICE_NAME"
    else
        cd "$INSTALL_DIR"
        nohup "./${BINARY_NAME}" > "${INSTALL_DIR}/subconv.log" 2>&1 &
    fi
    ok "SubConv 已启动"
}

# ============ 停止 ============
do_stop() {
    if ! is_running; then
        warn "SubConv 未在运行"
        return
    fi
    if [ "$HAS_SYSTEMD" -eq 1 ]; then
        systemctl stop "$SERVICE_NAME"
    else
        pkill -x "$BINARY_NAME" 2>/dev/null || true
    fi
    ok "SubConv 已停止"
}

# ============ 重启 ============
do_restart() {
    if ! is_installed; then
        error "SubConv 未安装"
    fi
    if [ "$HAS_SYSTEMD" -eq 1 ]; then
        systemctl restart "$SERVICE_NAME"
    else
        do_stop
        do_start
        return
    fi
    ok "SubConv 已重启"
}

# ============ 状态 ============
do_status() {
    printf "\n"
    if is_installed; then
        printf "  安装状态: ${GREEN}已安装${NC}\n"
        printf "  安装目录: %s\n" "$INSTALL_DIR"
    else
        printf "  安装状态: ${RED}未安装${NC}\n"
        return
    fi

    if is_running; then
        printf "  运行状态: ${GREEN}运行中${NC}\n"
    else
        printf "  运行状态: ${RED}已停止${NC}\n"
    fi

    if [ "$HAS_SYSTEMD" -eq 1 ]; then
        if systemctl is-enabled --quiet "$SERVICE_NAME" 2>/dev/null; then
            printf "  开机自启: ${GREEN}已启用${NC}\n"
        else
            printf "  开机自启: ${YELLOW}未启用${NC}\n"
        fi
    fi
    printf "\n"
}

# ============ 日志 ============
do_log() {
    if [ "$HAS_SYSTEMD" -eq 1 ]; then
        journalctl -u "$SERVICE_NAME" -f --no-hostname -o cat
    elif [ -f "${INSTALL_DIR}/subconv.log" ]; then
        tail -f "${INSTALL_DIR}/subconv.log"
    else
        warn "没有可用的日志"
    fi
}

# ============ 配置 TLS ============
do_tls() {
    if ! is_installed; then
        error "SubConv 未安装"
    fi

    if [ ! -f "$CONFIG_FILE" ]; then
        warn "配置文件不存在，先启动一次服务以生成默认配置"
        return
    fi

    printf "\n"
    printf "${BLUE}配置 TLS 证书${NC}\n"
    printf "  证书通常由 certbot (Let's Encrypt) 或 acme.sh 生成\n"
    printf "  certbot 默认路径: /etc/letsencrypt/live/域名/fullchain.pem\n"
    printf "  acme.sh 默认路径: ~/.acme.sh/域名_ecc/fullchain.cer\n"
    printf "\n"

    printf "请输入证书文件路径 (fullchain.pem): "
    read -r cert_path < /dev/tty
    cert_path=$(echo "$cert_path" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
    if [ -z "$cert_path" ]; then
        warn "已取消 TLS 配置"
        return
    fi
    if [ ! -f "$cert_path" ]; then
        warn "证书文件不存在: $cert_path"
        printf "是否继续写入配置（稍后再放置证书）？[y/N]: "
        read -r answer < /dev/tty
        case "$answer" in
            [yY]|[yY][eE][sS]) ;;
            *) return ;;
        esac
    fi

    printf "请输入私钥文件路径 (privkey.pem): "
    read -r key_path < /dev/tty
    key_path=$(echo "$key_path" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
    if [ -z "$key_path" ]; then
        warn "已取消 TLS 配置"
        return
    fi
    if [ ! -f "$key_path" ]; then
        warn "私钥文件不存在: $key_path"
        printf "是否继续写入配置（稍后再放置私钥）？[y/N]: "
        read -r answer < /dev/tty
        case "$answer" in
            [yY]|[yY][eE][sS]) ;;
            *) return ;;
        esac
    fi

    # 读取当前监听端口
    current_port=$(grep '^listen:' "$CONFIG_FILE" | sed 's/.*":\([0-9]*\)".*/\1/')
    if [ -z "$current_port" ]; then
        current_port="8866"
    fi
    printf "监听端口 [${current_port}]: "
    read -r new_port < /dev/tty
    new_port=$(echo "$new_port" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')

    # 更新配置文件
    # 使用 sed 替换或追加 tls-cert / tls-key
    if grep -q '^tls-cert:' "$CONFIG_FILE" 2>/dev/null; then
        sed -i "s|^tls-cert:.*|tls-cert: \"$cert_path\"|" "$CONFIG_FILE"
    elif grep -q '^# tls-cert:' "$CONFIG_FILE" 2>/dev/null; then
        sed -i "s|^# tls-cert:.*|tls-cert: \"$cert_path\"|" "$CONFIG_FILE"
    else
        sed -i "/^listen:/a tls-cert: \"$cert_path\"" "$CONFIG_FILE"
    fi

    if grep -q '^tls-key:' "$CONFIG_FILE" 2>/dev/null; then
        sed -i "s|^tls-key:.*|tls-key: \"$key_path\"|" "$CONFIG_FILE"
    elif grep -q '^# tls-key:' "$CONFIG_FILE" 2>/dev/null; then
        sed -i "s|^# tls-key:.*|tls-key: \"$key_path\"|" "$CONFIG_FILE"
    else
        sed -i "/^tls-cert:/a tls-key: \"$key_path\"" "$CONFIG_FILE"
    fi

    # 仅在用户输入了新端口时才修改
    if [ -n "$new_port" ] && [ "$new_port" != "$current_port" ]; then
        sed -i "s|^listen:.*|listen: \":$new_port\"|" "$CONFIG_FILE"
    fi

    ok "TLS 配置已写入"
    printf "  证书文件: %s\n" "$cert_path"
    printf "  私钥文件: %s\n" "$key_path"
    if [ -n "$new_port" ] && [ "$new_port" != "$current_port" ]; then
        printf "  监听端口: %s (已修改)\n" "$new_port"
    else
        printf "  监听端口: %s (未修改)\n" "$current_port"
    fi

    if is_running; then
        printf "${YELLOW}是否重启服务使 TLS 生效？[Y/n]: ${NC}"
        read -r answer < /dev/tty
        case "$answer" in
            [nN]|[nN][oO]) info "跳过重启，修改将在下次启动时生效" ;;
            *) do_restart ;;
        esac
    fi
}

# ============ 编辑配置 ============
do_config() {
    if ! is_installed; then
        error "SubConv 未安装"
    fi

    if [ ! -f "$CONFIG_FILE" ]; then
        warn "配置文件不存在，先启动一次服务以生成默认配置"
        return
    fi

    # 选择编辑器
    if command -v vim >/dev/null 2>&1; then
        EDITOR_CMD="vim"
    elif command -v vi >/dev/null 2>&1; then
        EDITOR_CMD="vi"
    elif command -v nano >/dev/null 2>&1; then
        EDITOR_CMD="nano"
    else
        error "未找到可用的编辑器 (vim/vi/nano)"
    fi

    info "使用 ${EDITOR_CMD} 编辑配置文件..."
    $EDITOR_CMD "$CONFIG_FILE" < /dev/tty

    if is_running; then
        printf "${YELLOW}配置已修改，是否重启服务使其生效？[Y/n]: ${NC}"
        read -r answer < /dev/tty
        case "$answer" in
            [nN]|[nN][oO]) info "跳过重启，修改将在下次启动时生效" ;;
            *) do_restart ;;
        esac
    else
        ok "配置已保存，启动服务后生效"
    fi
}

# ============ 卸载 ============
do_uninstall() {
    if ! is_installed; then
        error "SubConv 未安装"
    fi

    printf "${RED}确认要卸载 SubConv 吗？[y/N]: ${NC}"
    read -r answer < /dev/tty
    case "$answer" in
        [yY]|[yY][eE][sS]) ;;
        *) info "取消卸载"; return ;;
    esac

    # 停止服务
    if is_running; then
        do_stop
    fi

    # 移除 systemd
    if [ "$HAS_SYSTEMD" -eq 1 ]; then
        systemctl disable "$SERVICE_NAME" 2>/dev/null || true
        rm -f "$SERVICE_FILE"
        systemctl daemon-reload
    fi

    # 询问是否保留配置
    if [ -f "$CONFIG_FILE" ]; then
        printf "${YELLOW}是否保留配置文件 ${CONFIG_FILE}？[Y/n]: ${NC}"
        read -r answer < /dev/tty
        case "$answer" in
            [nN]|[nN][oO])
                rm -rf "$INSTALL_DIR"
                ok "已删除全部文件"
                ;;
            *)
                BACKUP="/tmp/subconv-config-backup.yaml"
                cp "$CONFIG_FILE" "$BACKUP"
                rm -rf "$INSTALL_DIR"
                ok "已删除程序文件，配置已备份到 $BACKUP"
                ;;
        esac
    else
        rm -rf "$INSTALL_DIR"
        ok "已删除全部文件"
    fi

    # 移除管理命令
    rm -f /usr/local/bin/subconv

    ok "SubConv 已卸载"
}

# ============ 安装管理脚本到本地 ============
install_management_script() {
    SCRIPT_URL="https://raw.githubusercontent.com/${REPO}/main/install.sh"
    if [ -n "$GITHUB_PROXY" ]; then
        SCRIPT_URL="${GITHUB_PROXY}${SCRIPT_URL}"
    fi
    info "正在下载管理脚本..."
    download "$SCRIPT_URL" "${INSTALL_DIR}/install.sh"
    chmod +x "${INSTALL_DIR}/install.sh"

    cat > /usr/local/bin/subconv <<'SCRIPT'
#!/bin/sh
exec /opt/subconv/install.sh "$@"
SCRIPT
    chmod +x /usr/local/bin/subconv
    ok "管理命令已安装: subconv {start|stop|restart|status|log|config|tls|uninstall}"
}

# ============ 菜单 ============
show_menu() {
    printf "\n"
    printf "${GREEN}========================================${NC}\n"
    printf "${GREEN}       SubConv 管理脚本${NC}\n"
    printf "${GREEN}========================================${NC}\n"
    printf "\n"
    printf "  ${GREEN}1.${NC} 安装 / 升级\n"
    printf "  ${GREEN}2.${NC} 启动\n"
    printf "  ${GREEN}3.${NC} 停止\n"
    printf "  ${GREEN}4.${NC} 重启\n"
    printf "  ${GREEN}5.${NC} 查看状态\n"
    printf "  ${GREEN}6.${NC} 查看日志\n"
    printf "  ${GREEN}7.${NC} 编辑配置\n"
    printf "  ${GREEN}8.${NC} 配置 TLS\n"
    printf "  ${GREEN}9.${NC} 卸载\n"
    printf "  ${GREEN}0.${NC} 退出\n"
    printf "\n"
    printf "请选择操作 [0-9]: "
    read -r choice < /dev/tty
    case "$choice" in
        1) do_install ;;
        2) do_start ;;
        3) do_stop ;;
        4) do_restart ;;
        5) do_status ;;
        6) do_log ;;
        7) do_config ;;
        8) do_tls ;;
        9) do_uninstall ;;
        0) exit 0 ;;
        *) warn "无效选项" ;;
    esac
}

# ============ 主入口 ============
main() {
    check_root
    check_os
    check_systemd

    # 处理 GitHub 代理参数（仅对纯 URL 参数生效）
    case "${1:-}" in
        http://*|https://*) GITHUB_PROXY="$1"; shift ;;
    esac

    # 命令行参数模式
    case "${1:-}" in
        install|upgrade)  do_install; install_management_script ;;
        start)            do_start ;;
        stop)             do_stop ;;
        restart)          do_restart ;;
        status)           do_status ;;
        log|logs)         do_log ;;
        config)           do_config ;;
        tls)              do_tls ;;
        uninstall|remove) do_uninstall ;;
        "")
            # 无参数：首次安装直接安装，否则显示菜单
            if ! is_installed; then
                do_install
                install_management_script
            else
                show_menu
            fi
            ;;
        *)
            printf "用法: subconv {install|start|stop|restart|status|log|config|tls|uninstall}\n"
            exit 1
            ;;
    esac
}

main "$@"
