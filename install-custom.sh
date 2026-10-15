#!/usr/bin/env bash
# One entry point for fresh installs and maintenance of standard installations.
set -Eeuo pipefail
umask 077
RELEASE=custom-hotreload-20261001
ARCHIVE_SHA=0cffb212f033b54eca40aee9b5f0f15cee728af83636cf90d624b81ec282e45d
BINARY_SHA=58ce7b14e5e2d23b6cd2765c2ac1771b1f247e4e7e2df1ea8e61a0d1bcbcdcda
BASE=https://github.com/xiaofujie369/xbord-node-v3/releases/download
UNIT=xboard-node-custom.service
fail() { printf '错误：%s\n' "$*" >&2; exit 1; }

# A freshly enabled timer can race the interactive validation. Wait for Certbot's
# own lock; never kill a renewal or delete its lock files.
certbot_run() {
    local attempt result
    for attempt in {1..12}; do
        if certbot "$@" > "$TMP_DIR/certbot-output" 2>&1; then
            cat "$TMP_DIR/certbot-output"
            return 0
        else
            result=$?
        fi
        cat "$TMP_DIR/certbot-output" >&2
        if ! grep -Fq 'Another instance of Certbot is already running' "$TMP_DIR/certbot-output"; then
            return "$result"
        fi
        echo 'Certbot 正在运行，10 秒后重试……' >&2
        sleep 10
    done
    fail 'Certbot 持续被占用；保留节点服务和续期配置，请稍后重新运行本脚本'
}

# Read only certificate paths; never print machine tokens. Instances may override
# the global certificate. Keep Certbot's live symlinks, not archive/*1.pem files.
configure_renewal() {
    python3 - /etc/xboard-node-custom/config.yml "$TMP_DIR/lineages" <<'CERTPATHS'
import pathlib, sys, yaml
config=yaml.safe_load(pathlib.Path(sys.argv[1]).read_text()) or {}
lineages=set()
for entry in [config]+(config.get('instances') or []):
    cert=entry.get('cert') or {}
    if cert.get('cert_mode') != 'file': continue
    full=pathlib.Path(cert.get('cert_file') or '')
    key=pathlib.Path(cert.get('key_file') or '')
    if not full.is_file() or not key.is_file():
        sys.exit('证书或私钥文件不存在；保留原配置，请先修复路径')
    if str(full.parent.parent) == '/etc/letsencrypt/live' and key.parent == full.parent:
        if full.name != 'fullchain.pem' or key.name != 'privkey.pem':
            sys.exit('Certbot 证书请使用 live/<证书名>/fullchain.pem 和 privkey.pem')
        if not pathlib.Path('/etc/letsencrypt/renewal', full.parent.name+'.conf').is_file():
            sys.exit('未找到证书对应的 Certbot 续期配置')
        lineages.add(str(full.parent))
    else:
        print('外部证书由原签发工具负责续期：'+str(full))
pathlib.Path(sys.argv[2]).write_text(''.join(p+'\n' for p in sorted(lineages)))
CERTPATHS
    [[ -s $TMP_DIR/lineages ]] || { echo '没有配置 Certbot 文件证书；跳过 Certbot 续期设置。'; return; }
    command -v certbot >/dev/null || fail '发现 Certbot 证书但未安装 certbot'
    install -m 600 "$TMP_DIR/lineages" /etc/xboard-node-custom/certbot-lineages
    install -d -m 755 /etc/letsencrypt/renewal-hooks/deploy
    cat > "$TMP_DIR/renew-hook" <<'RENEWHOOK'
#!/bin/sh
set -eu
# Only restart for a certificate actually used by this node installation.
[ -n "${RENEWED_LINEAGE:-}" ] || exit 0
grep -Fxq -- "$RENEWED_LINEAGE" /etc/xboard-node-custom/certbot-lineages || exit 0
openssl x509 -in "$RENEWED_LINEAGE/fullchain.pem" -noout -checkend 0 >/dev/null
cert_key=$(openssl x509 -in "$RENEWED_LINEAGE/fullchain.pem" -pubkey -noout)
private_key=$(openssl pkey -in "$RENEWED_LINEAGE/privkey.pem" -pubout)
[ "$cert_key" = "$private_key" ] || { echo 'Certificate/private key mismatch' >&2; exit 1; }
systemctl try-restart xboard-node-custom.service
RENEWHOOK
    install -m 700 "$TMP_DIR/renew-hook" /etc/letsencrypt/renewal-hooks/deploy/xboard-node-custom.sh
    systemctl enable --now certbot.timer
    while IFS= read -r lineage; do
        printf '证书固定路径：%s/fullchain.pem\n私钥固定路径：%s/privkey.pem\n' "$lineage" "$lineage"
        echo '正在验证自动续期及成功后的证书加载（可能需要数分钟）……'
        certbot_run renew --cert-name "${lineage##*/}" --dry-run --run-deploy-hooks --non-interactive --no-random-sleep-on-renew
    done < "$TMP_DIR/lineages"
    systemctl is-active --quiet certbot.timer
    systemctl is-active --quiet "$UNIT"
    echo '自动续期与续期后加载验证通过；成功续期会短暂重启节点连接。'
}

if [[ ${1:-} == --help ]]; then
    echo 'Debian/Ubuntu amd64 一体安装：新机交互配置；已有标准安装保留配置、按需升级并验证证书续期。'
    exit 0
fi
[[ $# == 0 ]] || fail '只支持交互安装或 --help'
[[ $EUID == 0 ]] || fail '请以 root 执行'
[[ $(uname -m) == x86_64 ]] || fail '当前发行包仅支持 x86_64/amd64'
command -v apt-get >/dev/null || fail '当前脚本仅支持 Debian/Ubuntu'
[[ -d /run/systemd/system ]] || fail '需要 systemd 系统'
exec 8>/run/lock/xboard-node-setup.lock
flock -n 8 || fail '已有安装或维护正在执行'
TMP_DIR=$(mktemp -d)
trap 'rm -rf -- "$TMP_DIR"' EXIT
if [[ -f /opt/xboard-node-custom/xboard-node && -f /etc/xboard-node-custom/config.yml && -f /etc/systemd/system/$UNIT ]]; then
    BACKUP=$(mktemp -d /root/xboard-node-setup-XXXXXXXX)
    cp -a /etc/xboard-node-custom "$BACKUP/config"
    cp -p /etc/systemd/system/$UNIT "$BACKUP/"
    if [[ -d /etc/letsencrypt ]]; then cp -a /etc/letsencrypt "$BACKUP/letsencrypt"; fi
    printf '检测到已有安装，保留配置；备份：%s\n' "$BACKUP"
    apt-get update
    DEBIAN_FRONTEND=noninteractive apt-get install -y ca-certificates curl python3 python3-yaml gzip openssl
    if [[ $(sha256sum /opt/xboard-node-custom/xboard-node | cut -d ' ' -f1) == "$BINARY_SHA" ]]; then
        echo "已是 $RELEASE，无需再次升级程序。"
        systemctl is-active --quiet "$UNIT" || fail '原服务未运行，请先检查日志；未主动启动'
    else
        # Pinned reviewed updater matches the release/checksums above.
        curl -fL --retry 3 --connect-timeout 15 --max-time 120 \
            https://raw.githubusercontent.com/xiaofujie369/xbord-node-v3/7893713344d301b07978c2c1e84d848181b20e39/upgrade-custom.sh -o "$TMP_DIR/upgrade.sh"
        echo "50bde59295de031d0cb425b285450693bd4e5dfee704961a673cb34531ea83a8  $TMP_DIR/upgrade.sh" | sha256sum -c -
        bash "$TMP_DIR/upgrade.sh"
    fi
    configure_renewal
    echo '已有安装维护完成，无需另行执行 upgrade-custom.sh。请更新订阅验证节点连接。'
    exit 0
fi
for path in /opt/xboard-node-custom /etc/xboard-node-custom /var/lib/xboard-node-custom /etc/systemd/system/$UNIT; do
    [[ ! -e $path ]] || fail "已存在 $path；为保留原服务，本脚本不覆盖安装"
done
# Refuse a second copy of the previously deployed test service.
[[ ! -e /etc/systemd/system/codex-dual-anytls.service ]] || fail '本机已有实测服务，不需要重复安装'
exec 3<>/dev/tty || fail '需要交互终端'
read -r -u 3 -p '面板根地址（https://panel.example.com）：' PANEL_URL
[[ $PANEL_URL == https://* && $PANEL_URL != *'#'* && $PANEL_URL != *'?'* ]] || fail '请输入 HTTPS 面板根地址'
read -r -u 3 -p '面板服务器 ID：' MACHINE_ID
[[ $MACHINE_ID =~ ^[1-9][0-9]*$ ]] || fail '服务器 ID 必须是正整数'
read -r -s -u 3 -p '服务器专用 token（输入隐藏）：' MACHINE_TOKEN
printf '\n' >&3
[[ -n $MACHINE_TOKEN ]] || fail 'token 不能为空'
read -r -u 3 -p '申请证书的域名（已解析到本机；留空跳过）：' CERT_DOMAIN
if [[ -n $CERT_DOMAIN ]]; then
    [[ $CERT_DOMAIN =~ ^[a-zA-Z0-9]([a-zA-Z0-9.-]*[a-zA-Z0-9])?$ && $CERT_DOMAIN == *.* ]] || fail '域名格式不正确'
    read -r -u 3 -p '证书联系邮箱：' CERT_EMAIL
    [[ $CERT_EMAIL == *@*.* && $CERT_EMAIL != -* ]] || fail '邮箱格式不正确'
    echo '将使用 Certbot standalone 申请证书，需要域名解析、TCP 80 放行，并同意 Let’s Encrypt 服务条款。' >&3
    read -r -u 3 -p '同意并申请？[y/N]：' CERT_AGREE
    [[ $CERT_AGREE == y || $CERT_AGREE == Y ]] || fail '未同意申请证书'
fi
apt-get update
DEBIAN_FRONTEND=noninteractive apt-get install -y ca-certificates curl python3 python3-yaml gzip iproute2 openssl
curl --fail --location --retry 3 --proto '=https' --tlsv1.2 \
    "$BASE/$RELEASE/xboard-node-linux-amd64.gz" -o "$TMP_DIR/node.gz"
echo "$ARCHIVE_SHA  $TMP_DIR/node.gz" | sha256sum -c -
gzip -dc "$TMP_DIR/node.gz" > "$TMP_DIR/xboard-node"
echo "$BINARY_SHA  $TMP_DIR/xboard-node" | sha256sum -c -
export PANEL_URL MACHINE_ID MACHINE_TOKEN CERT_DOMAIN
python3 - "$TMP_DIR/config.yml" <<'PY'
import json, os, sys, urllib.parse
url=os.environ['PANEL_URL'].rstrip('/')
parts=urllib.parse.urlsplit(url)
if not parts.hostname or parts.username or parts.password or parts.path:
    sys.exit('面板地址必须是根地址，不含管理路径、用户名或密码')
config={'panel':{'url':url}, 'machine':{'machine_id':int(os.environ['MACHINE_ID']),
        'token':os.environ['MACHINE_TOKEN']}, 'kernel':{'type':'singbox'},
        'log':{'level':'info'}, 'health_port':0}
domain=os.environ.get('CERT_DOMAIN','')
if domain:
    cert_dir='/etc/letsencrypt/live/'+domain
    config['cert']={'cert_mode':'file','cert_file':cert_dir+'/fullchain.pem',
                    'key_file':cert_dir+'/privkey.pem'}
# JSON without escaped slashes is also valid YAML for this node parser.
with open(sys.argv[1], 'w') as f: json.dump(config,f)
PY
unset MACHINE_TOKEN
if [[ -n $CERT_DOMAIN ]]; then
    [[ -z $(ss -H -ltn 'sport = :80') ]] || fail 'TCP 80 已占用，请使用已有网站的证书申请方式'
    DEBIAN_FRONTEND=noninteractive apt-get install -y certbot
    certbot_run certonly --standalone --cert-name "$CERT_DOMAIN" -d "$CERT_DOMAIN" -m "$CERT_EMAIL" --agree-tos --non-interactive --keep-until-expiring
    systemctl enable --now certbot.timer
fi
install -d -m 700 /etc/xboard-node-custom /var/lib/xboard-node-custom
install -d -m 755 /opt/xboard-node-custom
install -m 755 "$TMP_DIR/xboard-node" /opt/xboard-node-custom/xboard-node
install -m 600 "$TMP_DIR/config.yml" /etc/xboard-node-custom/config.yml
MEM_MIB=$(awk '/MemTotal:/ {print int($2/1024)}' /proc/meminfo)
GO_MEM_MIB=$((MEM_MIB / 3))
(( GO_MEM_MIB > 1024 )) && GO_MEM_MIB=1024
(( GO_MEM_MIB >= 64 )) || fail '可用总内存过小，至少需要 192 MiB'
cat > /etc/systemd/system/$UNIT <<SERVICE
[Unit]
Description=Custom Xboard-Node
After=network-online.target
Wants=network-online.target
[Service]
Type=simple
ExecStart=/opt/xboard-node-custom/xboard-node -c /etc/xboard-node-custom/config.yml
WorkingDirectory=/var/lib/xboard-node-custom
Environment=GOMEMLIMIT=${GO_MEM_MIB}MiB
Environment=GOGC=50
UMask=0077
Restart=on-failure
RestartSec=5
LimitNOFILE=65535
[Install]
WantedBy=multi-user.target
SERVICE
chmod 644 /etc/systemd/system/$UNIT
systemctl daemon-reload
systemctl enable --now "$UNIT"
sleep 3
if ! systemctl is-active --quiet "$UNIT"; then
    systemctl disable --now "$UNIT"
    fail '服务未启动，配置已保留；请执行 journalctl -u xboard-node-custom -n 50 排查，分享前隐藏凭据'
fi
configure_renewal
echo "安装版本：$RELEASE，无需再执行 upgrade-custom.sh。"
echo '服务已安装并启动（尚不代表节点已连通）。请在面板把节点绑定到此服务器并分配权限组，再更新订阅测试。'
echo '请按面板节点的实际端口放行 TCP/UDP；standalone 证书续期需要 TCP 80。本脚本不修改防火墙。'
if [[ -n $CERT_DOMAIN ]]; then
    printf '面板 file 证书路径：\n/etc/letsencrypt/live/%s/fullchain.pem\n/etc/letsencrypt/live/%s/privkey.pem\n' "$CERT_DOMAIN" "$CERT_DOMAIN"
fi
echo 'FlClash 使用标准 AnyTLS / Hysteria2；AnyTLS REALITY 需要兼容 sing-box。主站需配置有效的 wss:// 地址及 WebSocket 反向代理；REST 轮询保留。'
