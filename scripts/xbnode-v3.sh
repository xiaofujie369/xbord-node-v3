#!/usr/bin/env bash
# xbord-node-v3 tested release helper for Debian/Ubuntu with systemd.
# Downloads the prebuilt release binaries built by GitHub Actions (checked
# against the SHA-256 values pinned below), then installs, upgrades, rolls
# back or uninstalls the node service. Nothing is compiled on the VPS.
#
#   xbnode-v3.sh install --panel-url URL --node-id N --node-type TYPE [--token-file F] [--binary F]
#                        [--port-range LO-HI [--listen-port P]]
#   xbnode-v3.sh upgrade [--binary F]          upgrade an existing installation in place
#   xbnode-v3.sh rollback [BACKUP_DIR]         restore the latest (or given) backup
#   xbnode-v3.sh port-range LO-HI [--listen-port P] | off
#                                              redirect a TCP+UDP port range to the node port
#   xbnode-v3.sh uninstall [--purge]           remove the service; --purge also deletes config
#   xbnode-v3.sh status
set -Eeuo pipefail
umask 077

RELEASE_TAG=v3.0.0
RELEASE_URL=https://github.com/xiaofujie369/xbord-node-v3/releases/download/$RELEASE_TAG
declare -A NODE_SHA256=(
    [amd64]=48a20dc86f00ee8f6050df54a616da519ca39eaa9d34b9a381cb9110e1c09c2f
    [arm64]=15bfcb32d91d4296549e13f35284d5f1700bc5b0b4854c2d822691ad9bcc9593
)
declare -A CLI_SHA256=(
    [amd64]=4ee54b1914a796b8543dfce4d2eeeca1f625748f5c0469045ea4d32c3a729755
    [arm64]=ad31c1b9412d46d6966809ec8b7e0cf7f6318845f49f1b757c10b5b7e619e540
)
WORK_DIR=${XBNODE_WORK_DIR:-/root/xbnode-v3-download}
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
# Port-range redirect: an nftables table of our own, loaded by a oneshot unit.
PR_DIR=/etc/xbnode-v3
PR_CONF=$PR_DIR/port-range.nft
PR_UNIT=xbnode-port-range.service
PR_TABLE=xbnode_portrange

say() { printf '==> %s\n' "$*"; }
fail() { printf '错误：%s\n' "$*" >&2; exit 1; }

PANEL_URL='' NODE_ID='' NODE_TYPE='' TOKEN_FILE='' BINARY='' CLI_BINARY='' PURGE=0 POS_ARG=''
PORT_RANGE='' LISTEN_PORT=''

cleanup() { rm -rf "$WORK_DIR"; }
trap cleanup EXIT

parse_args() {
    while (($#)); do
        case $1 in
        --panel-url) PANEL_URL=${2:-}; shift 2 ;;
        --node-id) NODE_ID=${2:-}; shift 2 ;;
        --node-type) NODE_TYPE=${2:-}; shift 2 ;;
        --token-file) TOKEN_FILE=${2:-}; shift 2 ;;
        --binary) BINARY=${2:-}; shift 2 ;;
        --purge) PURGE=1; shift ;;
        --port-range) PORT_RANGE=${2:-}; shift 2 ;;
        --listen-port) LISTEN_PORT=${2:-}; shift 2 ;;
        -*) fail "未知参数 $1" ;;
        *) POS_ARG=$1; shift ;;
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

# fetch NAME SHA256: downloads one release asset into $WORK_DIR and verifies it.
fetch() {
    local out=$WORK_DIR/$1
    curl -fsSL --retry 3 --proto '=https' -o "$out" "$RELEASE_URL/$1" || fail "下载失败：$RELEASE_URL/$1"
    echo "$2  $out" | sha256sum -c --quiet - || fail "$1 的 SHA-256 校验失败，已停止"
    chmod 755 "$out"
}

obtain_binary() {
    if [[ -n $BINARY ]]; then
        [[ -f $BINARY ]] || fail "找不到 $BINARY"
        chmod 755 "$BINARY"
    else
        ensure_packages ca-certificates curl
        rm -rf "$WORK_DIR" && mkdir -p "$WORK_DIR"
        say "下载 GitHub 编译好的 $RELEASE_TAG（$ARCH）并校验 SHA-256"
        fetch "xboard-node-linux-$ARCH" "${NODE_SHA256[$ARCH]}"
        fetch "xbctl-linux-$ARCH" "${CLI_SHA256[$ARCH]}"
        BINARY=$WORK_DIR/xboard-node-linux-$ARCH CLI_BINARY=$WORK_DIR/xbctl-linux-$ARCH
    fi
    "$BINARY" -v >/dev/null || fail "$BINARY 无法运行（架构不符或文件损坏）"
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

# Sets PR_LO, PR_HI and PR_TO (the node port; defaults to the first port of the range,
# which is also what XBoard sends as server_port when a range is entered there).
parse_port_range() {
    [[ $1 =~ ^([0-9]{1,5})-([0-9]{1,5})$ ]] || fail '端口范围格式应为「起始-结束」，例如 8443-11443'
    PR_LO=$((10#${BASH_REMATCH[1]})) PR_HI=$((10#${BASH_REMATCH[2]}))
    ((PR_LO >= 1 && PR_HI <= 65535 && PR_LO < PR_HI)) || fail '端口范围需在 1-65535 之间，且起始端口小于结束端口'
    PR_TO=${LISTEN_PORT:-$PR_LO}
    [[ $PR_TO =~ ^[0-9]{1,5}$ ]] && ((10#$PR_TO >= 1 && 10#$PR_TO <= 65535)) || fail '--listen-port 需为 1-65535 的端口'
    PR_TO=$((10#$PR_TO))
}

# Redirecting a port another program listens on would silently steal its traffic.
check_range_free() {
    command -v ss >/dev/null || ensure_packages iproute2
    local p busy=()
    while read -r p; do
        ((p >= PR_LO && p <= PR_HI && p != PR_TO)) && busy+=("$p")
    done < <(ss -Hlntu | awk '{n = split($5, a, ":"); print a[n]}' | sort -un)
    ((${#busy[@]} == 0)) || fail "端口范围内已有其他程序在监听：${busy[*]}。转发后它们会收不到连接，请换一个范围"
}

apply_port_range() {
    ensure_packages nftables
    check_range_free
    local nft; nft=$(command -v nft)
    install -d -m 755 "$PR_DIR"
    cat > "$PR_CONF.new" <<NFT
# 由 xbnode-v3.sh 生成：把 $PR_LO-$PR_HI 的 TCP/UDP 连接转到节点端口 $PR_TO（IPv4 和 IPv6）
table inet $PR_TABLE
delete table inet $PR_TABLE
table inet $PR_TABLE {
    chain prerouting {
        type nat hook prerouting priority dstnat; policy accept;
        meta l4proto { tcp, udp } th dport $PR_LO-$PR_HI redirect to :$PR_TO
    }
}
NFT
    "$nft" -c -f "$PR_CONF.new" || { rm -f "$PR_CONF.new"; fail '系统不支持这条 nftables 转发规则（内核需 5.2 以上）'; }
    chmod 644 "$PR_CONF.new" && mv -f "$PR_CONF.new" "$PR_CONF"
    cat > "/etc/systemd/system/$PR_UNIT" <<UNIT
[Unit]
Description=xbord-node port range redirect (nftables)
Wants=network-pre.target
After=network-pre.target nftables.service
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=$nft -f $PR_CONF
ExecStop=$nft delete table inet $PR_TABLE
[Install]
WantedBy=multi-user.target
UNIT
    chmod 644 "/etc/systemd/system/$PR_UNIT"
    systemctl daemon-reload
    systemctl enable "$PR_UNIT" >/dev/null 2>&1
    systemctl restart "$PR_UNIT" && "$nft" list table inet "$PR_TABLE" >/dev/null || fail '转发规则加载失败，请查看 journalctl -u xbnode-port-range'
    say "已开启端口范围：$PR_LO-$PR_HI（TCP+UDP）→ 节点端口 $PR_TO，开机自动生效"
    say "面板「连接端口」填 $PR_LO-$PR_HI、「服务端口」填 $PR_TO；云服务商安全组放行 $PR_LO-$PR_HI 的 TCP 和 UDP"
    ss -Hlnt "sport = :$PR_TO" | grep -q . ||
        say "提示：节点当前没有在 $PR_TO 端口监听，请确认面板「服务端口」填的是 $PR_TO"
}

remove_port_range() {
    [[ -f /etc/systemd/system/$PR_UNIT ]] || return 1
    systemctl disable --now "$PR_UNIT" >/dev/null 2>&1 || true
    nft delete table inet "$PR_TABLE" 2>/dev/null || true
    rm -f "/etc/systemd/system/$PR_UNIT" "$PR_CONF"
    rmdir "$PR_DIR" 2>/dev/null || true
    systemctl daemon-reload
    say '已关闭端口范围转发'
}

cmd_port_range() {
    case $POS_ARG in
    off) remove_port_range || say '本机没有开启端口范围转发' ;;
    '') fail '用法：xbnode-v3.sh port-range 8443-11443 [--listen-port 8443]，关闭用 port-range off' ;;
    *)
        [[ $(detect_layout) != none ]] || fail '没有找到已安装的节点，请先安装'
        parse_port_range "$POS_ARG"
        apply_port_range
        ;;
    esac
}

cmd_upgrade() {
    LAYOUT=$(detect_layout)
    [[ $LAYOUT != none ]] || fail '没有找到已安装的节点；新机器请用 install'
    set_layout_vars "$LAYOUT"
    say "检测到 $LAYOUT 布局：$BIN（服务 $UNIT），当前版本：$("$BIN" -v 2>&1 || true)"
    systemctl is-active --quiet "$UNIT" || say "提示：$UNIT 当前未运行，升级后会尝试启动"
    obtain_binary   # the old node keeps serving while this downloads
    backup_install
    say '替换程序并重启服务（实测中断通常不到 1 秒）'
    replace_file "$BINARY" "$BIN"
    [[ -n $CLI && -n $CLI_BINARY ]] && replace_file "$CLI_BINARY" "$CLI"
    systemctl restart "$UNIT"
    if wait_healthy "$UNIT"; then
        say "升级完成：$("$BIN" -v)。备份保留在 $BACKUP"
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
    [[ -z $PORT_RANGE ]] || parse_port_range "$PORT_RANGE"
    local token
    if [[ -n $TOKEN_FILE ]]; then
        token=$(tr -d '\r\n' < "$TOKEN_FILE")
    else
        exec 3<>/dev/tty || fail '需要交互终端输入通讯密钥，或使用 --token-file'
        read -r -s -u 3 -p '请粘贴面板「系统配置 → 节点配置 → 通讯密钥」（输入不显示）：' token
        printf '\n' >&3
    fi
    [[ -n $token ]] || fail '通讯密钥不能为空'
    [[ -z $PORT_RANGE ]] || check_range_free
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
        [[ -z $PORT_RANGE ]] || apply_port_range
    else
        journalctl -u "$UNIT" -n 30 --no-pager || true
        fail '服务启动后未能稳定运行，请按指南「排错」检查面板地址、节点 ID、类型和通讯密钥'
    fi
}

cmd_rollback() {
    local dir=$POS_ARG
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
    remove_port_range || true
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
    if [[ -f $PR_CONF ]]; then
        echo "端口范围：$(grep -o 'dport [0-9-]* redirect to :[0-9]*' "$PR_CONF" | sed 's/dport //; s/ redirect to :/ → /')（$(systemctl is-active "$PR_UNIT" || true)）"
    fi
}

main() {
    local cmd=${1:-}
    shift || true
    parse_args "$@"
    case $cmd in
    install) preflight; cmd_install ;;
    upgrade) preflight; cmd_upgrade ;;
    rollback) preflight; cmd_rollback ;;
    port-range) preflight; cmd_port_range ;;
    uninstall) preflight; cmd_uninstall ;;
    status) cmd_status ;;
    *) sed -n '2,14p' "$0"; exit 2 ;;
    esac
}
main "$@"
