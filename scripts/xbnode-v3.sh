#!/usr/bin/env bash
# xbord-node-v3 tested release helper for Debian/Ubuntu with systemd.
# Builds the pinned, tested source commit (or takes a prebuilt binary), then
# installs, upgrades, rolls back or uninstalls the node service.
#
#   xbnode-v3.sh build                         build only, print the binary path
#   xbnode-v3.sh install --panel-url URL --node-id N --node-type TYPE [--token-file F] [--binary F]
#   xbnode-v3.sh upgrade [--binary F]          upgrade an existing installation in place
#   xbnode-v3.sh rollback [BACKUP_DIR]         restore the latest (or given) backup
#   xbnode-v3.sh uninstall [--purge]           remove the service; --purge also deletes config
#   xbnode-v3.sh status
#
# Common options: --keep-build (keep the build directory), --no-temp-swap
# (never create a temporary swap file for the build).
set -Eeuo pipefail
umask 077

REPO_URL=https://github.com/xiaofujie369/xbord-node-v3.git
BRANCH=release/xbord-node-v3-tested
COMMIT=7dc66626bd7b4c4fa199716abafaba88be0639d1
GO_VERSION=1.26.10
declare -A GO_SHA256=(
    [amd64]=ccfe7ae822d2c2f41f4fe7c956199967a4dac22d0ee0d2b2bfdb440aab3b20c5
    [arm64]=3f4227ce7b206a513e794f1393adb473a07b46e94d18bd15d1c684bbb7319797
)
BUILD_TAGS="with_quic with_utls with_wireguard with_acme with_clash_api"
BUILD_DIR=${XBNODE_BUILD_DIR:-/root/xbnode-v3-build}
BACKUP_ROOT=/root

# Layout created by install-custom.sh (also used for fresh installs here).
CUSTOM_BIN=/opt/xboard-node-custom/xboard-node
CUSTOM_CONF=/etc/xboard-node-custom/config.yml
CUSTOM_UNIT=xboard-node-custom.service
# Layout created by the upstream-style install.sh.
STD_BIN=/usr/local/bin/xboard-node
STD_CLI=/usr/local/bin/xbctl
STD_CONF=/etc/xboard-node/config.yml
STD_UNIT=xboard-node.service

say() { printf '==> %s\n' "$*"; }
fail() { printf '错误：%s\n' "$*" >&2; exit 1; }

PANEL_URL='' NODE_ID='' NODE_TYPE='' TOKEN_FILE='' BINARY='' KEEP_BUILD=0 TEMP_SWAP=1 PURGE=0 ROLLBACK_DIR=''
SWAPFILE=''

cleanup() {
    if [[ -n $SWAPFILE && -f $SWAPFILE ]]; then
        swapoff "$SWAPFILE" 2>/dev/null || true
        rm -f "$SWAPFILE"
    fi
}
trap cleanup EXIT

parse_args() {
    while (($#)); do
        case $1 in
        --panel-url) PANEL_URL=${2:-}; shift 2 ;;
        --node-id) NODE_ID=${2:-}; shift 2 ;;
        --node-type) NODE_TYPE=${2:-}; shift 2 ;;
        --token-file) TOKEN_FILE=${2:-}; shift 2 ;;
        --binary) BINARY=${2:-}; shift 2 ;;
        --keep-build) KEEP_BUILD=1; shift ;;
        --no-temp-swap) TEMP_SWAP=0; shift ;;
        --purge) PURGE=1; shift ;;
        -*) fail "未知参数 $1" ;;
        *) ROLLBACK_DIR=$1; shift ;;
        esac
    done
}

preflight() {
    [[ $EUID == 0 ]] || fail '请用 root 执行'
    command -v apt-get >/dev/null || fail '仅支持 Debian/Ubuntu（需要 apt-get）'
    [[ -d /run/systemd/system ]] || fail '需要 systemd'
    case $(uname -m) in
    x86_64) ARCH=amd64 ;;
    aarch64) ARCH=arm64 ;;
    *) fail "不支持的 CPU 架构 $(uname -m)" ;;
    esac
    exec 9>/run/lock/xbnode-v3.lock
    flock -n 9 || fail '已有另一个安装/升级任务在运行'
}

# Prints "custom", "standard" or "none".
detect_layout() {
    local c=0 s=0
    [[ -f $CUSTOM_BIN && -f $CUSTOM_CONF && -f /etc/systemd/system/$CUSTOM_UNIT ]] && c=1
    [[ -f $STD_BIN && -f $STD_CONF && -f /etc/systemd/system/$STD_UNIT ]] && s=1
    if ((c && s)); then fail '同时检测到两种安装，请手动处理'; fi
    if ((c)); then echo custom; elif ((s)); then echo standard; else echo none; fi
}

set_layout_vars() {
    case $1 in
    custom) BIN=$CUSTOM_BIN CONF=$CUSTOM_CONF UNIT=$CUSTOM_UNIT CLI='' ;;
    standard) BIN=$STD_BIN CONF=$STD_CONF UNIT=$STD_UNIT CLI=$STD_CLI ;;
    esac
}

ensure_packages() {
    local missing=()
    for p in "$@"; do dpkg -s "$p" >/dev/null 2>&1 || missing+=("$p"); done
    ((${#missing[@]} == 0)) && return
    say "安装依赖：${missing[*]}"
    apt-get update -qq
    DEBIAN_FRONTEND=noninteractive apt-get install -y -qq "${missing[@]}" >/dev/null
}

# The build peaks around 1.1 GB of memory and needs ~5 GB of disk.
prepare_build_resources() {
    mkdir -p "$BUILD_DIR"
    local avail_kb mem_mb
    avail_kb=$(df --output=avail -k "$BUILD_DIR" | tail -1)
    ((avail_kb >= 6 * 1024 * 1024)) || fail "编译需要至少 6 GB 可用磁盘（$BUILD_DIR 所在分区不足）"
    mem_mb=$(awk '/MemTotal|SwapTotal/ {s+=$2} END {print int(s/1024)}' /proc/meminfo)
    local need=${XBNODE_MIN_MEM_MB:-2600}
    if ((mem_mb < need)); then
        ((TEMP_SWAP)) || fail "内存+交换仅 ${mem_mb} MB，编译至少需要 ${need} MB；可去掉 --no-temp-swap 或改用 --binary"
        SWAPFILE=$BUILD_DIR/temp-build.swap
        say "内存+交换仅 ${mem_mb} MB，临时创建 2 GB 交换文件（编译结束自动删除）"
        fallocate -l 2G "$SWAPFILE" 2>/dev/null || dd if=/dev/zero of="$SWAPFILE" bs=1M count=2048 status=none
        chmod 600 "$SWAPFILE"
        mkswap "$SWAPFILE" >/dev/null
        swapon "$SWAPFILE" || fail '无法启用临时交换文件；请改用 --binary 安装在别处编译好的程序'
    fi
}

build_binary() {
    ensure_packages ca-certificates curl git
    prepare_build_resources
    local go_tgz=$BUILD_DIR/go.tar.gz
    say "下载 Go $GO_VERSION 并校验 SHA-256"
    curl -fsSL --retry 3 --proto '=https' -o "$go_tgz" "https://go.dev/dl/go${GO_VERSION}.linux-${ARCH}.tar.gz"
    echo "${GO_SHA256[$ARCH]}  $go_tgz" | sha256sum -c --quiet -
    rm -rf "$BUILD_DIR/go" && tar -C "$BUILD_DIR" -xzf "$go_tgz" && rm -f "$go_tgz"

    say "获取源码 $BRANCH @ ${COMMIT:0:7}"
    rm -rf "$BUILD_DIR/src"
    git clone -q --filter=blob:none --branch "$BRANCH" "$REPO_URL" "$BUILD_DIR/src"
    git -C "$BUILD_DIR/src" checkout -q --detach "$COMMIT"
    [[ $(git -C "$BUILD_DIR/src" rev-parse HEAD) == "$COMMIT" ]] || fail '源码提交与固定版本不一致'

    say '开始编译（低优先级运行，约 5-15 分钟，不影响正在运行的节点继续服务）'
    local procs=1 mem_mb
    mem_mb=$(awk '/MemTotal/ {print int($2/1024)}' /proc/meminfo)
    ((mem_mb >= 3000)) && procs=2
    (
        cd "$BUILD_DIR/src"
        export PATH=$BUILD_DIR/go/bin:$PATH GOPATH=$BUILD_DIR/gopath GOCACHE=$BUILD_DIR/gocache \
            GOTOOLCHAIN=local CGO_ENABLED=0 GOFLAGS=-p=$procs GOMAXPROCS=$procs
        # The commit time (not the wall clock) keeps the build reproducible: the
        # same commit gives a byte-identical binary on every machine.
        local ld built
        built=$(TZ=UTC git log -1 --date=format-local:%Y-%m-%dT%H:%M:%SZ --format=%cd)
        ld="-s -w -buildid= -X main.version=v3-tested-${COMMIT:0:7} -X main.buildTime=$built"
        nice -n 19 go build -trimpath -ldflags "$ld" -tags "$BUILD_TAGS" -o "$BUILD_DIR/xboard-node" ./cmd/xboard-node
        nice -n 19 go build -trimpath -ldflags "$ld" -o "$BUILD_DIR/xbctl" ./cmd/xbctl
    )
    "$BUILD_DIR/xboard-node" -v
    BINARY=$BUILD_DIR/xboard-node
    say "编译完成：$BINARY  sha256=$(sha256sum "$BINARY" | cut -d' ' -f1)"
}

clean_build() {
    if ((KEEP_BUILD == 0)) && [[ -d $BUILD_DIR ]]; then
        rm -rf "$BUILD_DIR/go" "$BUILD_DIR/gopath" "$BUILD_DIR/gocache" "$BUILD_DIR/src"
    fi
}

obtain_binary() {
    if [[ -n $BINARY ]]; then
        [[ -f $BINARY ]] || fail "找不到 $BINARY"
        chmod 755 "$BINARY"
        "$BINARY" -v >/dev/null || fail "$BINARY 无法运行（架构不符或文件损坏）"
    else
        build_binary
    fi
}

# Waits until the unit has stayed active for 8 consecutive seconds.
wait_healthy() {
    local stable=0
    for _ in $(seq 1 30); do
        sleep 1
        if systemctl is-active --quiet "$1"; then
            ((++stable >= 8)) && return 0
        else
            stable=0
        fi
    done
    return 1
}

backup_install() {
    BACKUP=$(mktemp -d "$BACKUP_ROOT/xbnode-v3-backup-$(date +%Y%m%d-%H%M%S)-XXXX")
    cp -p "$BIN" "$BACKUP/xboard-node"
    [[ -n $CLI && -f $CLI ]] && cp -p "$CLI" "$BACKUP/xbctl"
    cp -a "$(dirname "$CONF")" "$BACKUP/config-dir"
    cp -p "/etc/systemd/system/$UNIT" "$BACKUP/"
    printf '%s\n%s\n%s\n%s\n' "$LAYOUT" "$BIN" "$UNIT" "$(dirname "$CONF")" > "$BACKUP/layout"
    say "已备份到 $BACKUP（程序、配置目录、systemd 服务文件）"
}

replace_file() { install -m 755 "$1" "$2.new" && mv -f "$2.new" "$2"; }

restore_backup() {
    # The upgrade never changes the configuration, so only program and unit are restored;
    # the config-dir copy in the backup is for manual recovery.
    local dir=$1 layout bin unit
    { read -r layout; read -r bin; read -r unit; } < "$dir/layout"
    replace_file "$dir/xboard-node" "$bin"
    [[ $layout == standard && -f $dir/xbctl ]] && replace_file "$dir/xbctl" "$STD_CLI"
    cp -p "$dir/$unit" "/etc/systemd/system/$unit"
    systemctl daemon-reload
    systemctl restart "$unit"
    wait_healthy "$unit"
}

cmd_upgrade() {
    LAYOUT=$(detect_layout)
    [[ $LAYOUT != none ]] || fail '没有找到已安装的节点；新机器请用 install'
    set_layout_vars "$LAYOUT"
    say "检测到 $LAYOUT 布局：$BIN（服务 $UNIT），当前版本：$("$BIN" -v 2>&1 || true)"
    systemctl is-active --quiet "$UNIT" || say "提示：$UNIT 当前未运行，升级后会尝试启动"
    obtain_binary   # the old node keeps serving while this builds
    backup_install
    say '替换程序并重启服务（实测中断通常不到 1 秒）'
    replace_file "$BINARY" "$BIN"
    [[ -n $CLI && -f $BUILD_DIR/xbctl && -z ${BINARY_FROM_ARG:-} ]] && replace_file "$BUILD_DIR/xbctl" "$CLI"
    systemctl restart "$UNIT"
    if wait_healthy "$UNIT"; then
        say "升级完成：$("$BIN" -v)。备份保留在 $BACKUP"
        clean_build
    else
        say '新版本未能稳定运行，正在自动回滚……'
        journalctl -u "$UNIT" -n 30 --no-pager 2>/dev/null || true
        restore_backup "$BACKUP" && fail "已回滚到原版本并恢复运行；备份：$BACKUP" ||
            fail "自动回滚后服务仍未运行，请按指南手动恢复；备份：$BACKUP"
    fi
}

cmd_install() {
    [[ $(detect_layout) == none ]] || fail '本机已经安装过节点；请用 upgrade'
    for p in /opt/xboard-node-custom /etc/xboard-node-custom /var/lib/xboard-node-custom "/etc/systemd/system/$CUSTOM_UNIT"; do
        [[ ! -e $p ]] || fail "已存在 $p，为避免覆盖，停止安装"
    done
    [[ $PANEL_URL =~ ^https?://[^/?#@]+$ ]] || fail '--panel-url 需为面板根地址，例如 https://panel.example.com（末尾不带 /）'
    [[ $NODE_ID =~ ^[1-9][0-9]*$ ]] || fail '--node-id 需为面板上节点的数字 ID'
    [[ $NODE_TYPE =~ ^(shadowsocks|vmess|vless|trojan|hysteria|tuic|anytls|socks|naive|http|mieru)$ ]] ||
        fail '--node-type 需与面板节点类型一致，例如 vless、trojan、shadowsocks'
    local token
    if [[ -n $TOKEN_FILE ]]; then
        token=$(tr -d '\r\n' < "$TOKEN_FILE")
    else
        exec 3<>/dev/tty || fail '需要交互终端输入通讯密钥，或使用 --token-file'
        read -r -s -u 3 -p '请粘贴面板「系统配置 → 节点配置 → 通讯密钥」（输入不显示）：' token
        printf '\n' >&3
    fi
    [[ -n $token ]] || fail '通讯密钥不能为空'
    ensure_packages ca-certificates curl python3
    obtain_binary

    LAYOUT=custom
    set_layout_vars custom
    install -d -m 700 /etc/xboard-node-custom /var/lib/xboard-node-custom
    install -d -m 755 /opt/xboard-node-custom
    replace_file "$BINARY" "$BIN"
    # JSON is valid YAML; the token never reaches the terminal or the logs.
    PANEL_URL=$PANEL_URL NODE_ID=$NODE_ID NODE_TYPE=$NODE_TYPE TOKEN=$token python3 - "$CONF" <<'PY'
import json, os, sys
cfg = {"panel": {"url": os.environ["PANEL_URL"], "token": os.environ["TOKEN"],
                 "node_id": int(os.environ["NODE_ID"]), "node_type": os.environ["NODE_TYPE"]},
       "kernel": {"type": "singbox"}, "log": {"level": "info"}}
with open(sys.argv[1], "w") as f:
    json.dump(cfg, f, indent=2)
PY
    chmod 600 "$CONF"
    local mem_mb go_mem
    mem_mb=$(awk '/MemTotal/ {print int($2/1024)}' /proc/meminfo)
    go_mem=$((mem_mb / 3)); ((go_mem > 1024)) && go_mem=1024; ((go_mem < 64)) && go_mem=64
    cat > "/etc/systemd/system/$UNIT" <<SERVICE
[Unit]
Description=Custom Xboard-Node
After=network-online.target
Wants=network-online.target
[Service]
Type=simple
ExecStart=$BIN -c $CONF
WorkingDirectory=/var/lib/xboard-node-custom
Environment=GOMEMLIMIT=${go_mem}MiB
Environment=GOGC=50
UMask=0077
Restart=on-failure
RestartSec=5
LimitNOFILE=65535
[Install]
WantedBy=multi-user.target
SERVICE
    chmod 644 "/etc/systemd/system/$UNIT"
    systemctl daemon-reload
    systemctl enable --now "$UNIT"
    if wait_healthy "$UNIT"; then
        say "安装完成并已设置开机自启：$("$BIN" -v)"
        say "查看日志：journalctl -u $UNIT -f"
        clean_build
    else
        journalctl -u "$UNIT" -n 30 --no-pager || true
        fail '服务启动后未能稳定运行，请按指南「排错」检查面板地址、节点 ID、类型和通讯密钥'
    fi
}

cmd_rollback() {
    local dir=$ROLLBACK_DIR
    [[ -n $dir ]] || dir=$(ls -1dt "$BACKUP_ROOT"/xbnode-v3-backup-* 2>/dev/null | head -1 || true)
    [[ -n $dir && -f $dir/layout && -f $dir/xboard-node ]] || fail '没有找到可用的备份目录'
    say "从 $dir 回滚"
    restore_backup "$dir" || fail '回滚后服务未能稳定运行，请查看 journalctl'
    local bin; bin=$(sed -n 2p "$dir/layout")
    say "回滚完成，当前版本：$("$bin" -v 2>&1)"
}

cmd_uninstall() {
    LAYOUT=$(detect_layout)
    [[ $LAYOUT != none ]] || fail '没有找到已安装的节点'
    set_layout_vars "$LAYOUT"
    systemctl disable --now "$UNIT" 2>/dev/null || true
    rm -f "/etc/systemd/system/$UNIT" "$BIN"
    [[ -n $CLI ]] && rm -f "$CLI"
    systemctl daemon-reload
    if ((PURGE)); then
        rm -rf "$(dirname "$CONF")"
        [[ $LAYOUT == custom ]] && rm -rf /opt/xboard-node-custom /var/lib/xboard-node-custom
        say '已卸载并删除配置'
    else
        say "已卸载；配置保留在 $(dirname "$CONF")（加 --purge 可一并删除）"
    fi
}

cmd_status() {
    LAYOUT=$(detect_layout)
    echo "安装布局：$LAYOUT"
    [[ $LAYOUT == none ]] && return
    set_layout_vars "$LAYOUT"
    echo "程序：$BIN"
    echo "版本：$("$BIN" -v 2>&1 || true)"
    echo "服务：$UNIT $(systemctl is-active "$UNIT" || true) / 开机自启 $(systemctl is-enabled "$UNIT" 2>/dev/null || true)"
}

main() {
    local cmd=${1:-}
    shift || true
    parse_args "$@"
    [[ -n $BINARY ]] && BINARY_FROM_ARG=1
    case $cmd in
    build) preflight; build_binary; KEEP_BUILD=1 ;;
    install) preflight; cmd_install ;;
    upgrade) preflight; cmd_upgrade ;;
    rollback) preflight; cmd_rollback ;;
    uninstall) preflight; cmd_uninstall ;;
    status) cmd_status ;;
    *) sed -n '2,15p' "$0"; exit 2 ;;
    esac
}
main "$@"
