# Debian 13 安装与实测记录

> **2026-10-21 更新**：经过完整测试的新版本 v3.0.0（GitHub 自动编译，VPS 上无需编译）请使用新的指南：[全新安装](FRESH_INSTALL_GUIDE_ZH.md)、[已有 VPS 升级](UPGRADE_GUIDE_ZH.md)。下面的 `install-custom.sh` 仍然只会安装旧版 `custom-hotreload-20261001`。

本仓库 main 是定制 Xboard-Node；面板配套快照在 `codex/live-panel-anytls` 分支。旧 `codex/anytls-reality-panel` 分支保留不动。不要把面板分支覆盖到节点目录。

本指南对应节点源码 417cd4f，面板源码 019677c。2026-09-27 已在 Debian 13 / amd64 上完成 AnyTLS TLS、AnyTLS REALITY、Hysteria2 TLS + Salamander 的实际代理连接和 HTTPS 下载验证。不是全部客户端、移动网络或同步功能的完整验收。

## 一键安装（Debian/Ubuntu amd64，新装或已有标准安装）

先在面板创建服务器，准备根地址、服务器 ID 和专用 token。在新 VPS 以 root 执行：

```bash
apt-get update && apt-get install -y ca-certificates curl
curl -fL https://raw.githubusercontent.com/xiaofujie369/xbord-node-v3/main/install-custom.sh -o /root/install-xboard-custom.sh && bash /root/install-xboard-custom.sh
```

新装时交互输入上述信息，可选申请域名证书（需要 DNS 正确、TCP 80 放行并同意证书服务条款）。脚本下载固定版本定制二进制，验证压缩包及二进制 SHA-256，创建受限配置及 systemd 开机服务，并自动把证书固定路径写入本机配置。已有标准安装自动转入备份、按需升级和续期检查，不重新询问或覆盖面板凭据。无需现场编译，当前只提供 amd64 包。脚本不创建面板节点、不修改防火墙；安装后仍需在面板绑定节点。面板设置的独立证书路径也必须存在。

当前发行包 `custom-hotreload-20261001` 包含面板路由修复和出站/DNS 自动更新修复，二进制 SHA-256 为 `58ce7b14e5e2d23b6cd2765c2ac1771b1f247e4e7e2df1ea8e61a0d1bcbcdcda`。详见 [路由修复记录](ROUTING_VERIFICATION_ZH.md) 和 [热更新实测与旧版升级](HOT_RELOAD_ZH.md)。2026-10-16 起新装和已有标准服务统一使用上面的安装入口，不必安装后再运行 `upgrade-custom.sh`。新入口增加续期演练与成功后的加载检查，见 [一体安装与证书续期](SETUP_RENEWAL_ZH.md)。以下手动步骤仅供自行编译部署时参考，不需要在一键安装后重复执行。

## 1. 准备与备份

以下适用于新的 Debian VPS。已有运行节点时，先备份配置、二进制和 systemd unit，检查端口占用；不要直接覆盖现有服务。当前已部署的测试 VPS 无需重复安装。

域名 A 记录指向 VPS，证书域名不要经过 CDN 代理。云安全组及主机防火墙需允许 TCP 80（HTTP-01 续期）、TCP 443、TCP 8443、UDP 2443，保留 SSH 管理端口。

```bash
apt-get update
apt-get install -y ca-certificates curl git certbot
ss -lntup
```

小内存 VPS 建议在另一台 Linux 构建机编译再上传。本次 715 MiB VPS 使用预编译的定制二进制；不能使用上游一键脚本或 Docker 镜像替代，否则会丢失本仓库修改。

## 2. 编译本仓库

构建机需已安装 Go 1.26（从 https://go.dev/dl/ 获取并校验官方摘要）。保持仓库 go.mod/go.sum 不变。

```bash
git clone --branch main https://github.com/xiaofujie369/xbord-node-v3.git
cd xbord-node-v3
export CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOTOOLCHAIN=go1.26.0 GOMAXPROCS=2
go build -p 2 -trimpath \
  -ldflags "-s -w -X main.version=$(git rev-parse --short HEAD)" \
  -tags 'with_quic with_utls with_wireguard with_acme with_clash_api' \
  -o xboard-node ./cmd/xboard-node
sha256sum xboard-node
scp xboard-node root@YOUR_VPS_IP:/root/xboard-node.new
```

在 VPS 再执行 `sha256sum /root/xboard-node.new`，与构建机核对。

## 3. 证书和面板节点

在 VPS 执行，换成自己的域名和邮箱；80 端口须空闲：

```bash
certbot certonly --standalone -d node.example.com \
  --agree-tos -m admin@example.com --non-interactive
systemctl enable --now certbot.timer
```

已有网站占用 80 时，使用现有 webroot/DNS 验证方式，不要停掉生产网站套用本命令。

在已安装配套修改的 Xboard 面板中创建服务器，记下服务器 ID 和专用 token。将以下三个独立测试节点绑定到这台服务器，并分配测试权限组及测试用户：

| 节点 | 面板设置 |
|---|---|
| AnyTLS TLS | 443；TLS 标准模式；SNI=node.example.com；不跳过证书校验 |
| AnyTLS REALITY | 8443；REALITY 模式；生成独立密钥对与 short ID；SNI 与目标证书匹配；dest=目标域名:443 |
| Hysteria2 | 2443；版本 2；标准 TLS；SNI=node.example.com；开启 Salamander 并生成随机混淆密码 |

两个标准 TLS 节点的证书选择 file 模式，路径：

- `/etc/letsencrypt/live/node.example.com/fullchain.pem`
- `/etc/letsencrypt/live/node.example.com/privkey.pem`

REALITY 不使用上述本地证书。目标须能从 VPS 访问并支持 TLS 1.3。本次用户选择的目标是 www.nvidia.com:443；客户端 SNI 必须同步。请保留私钥在面板/节点端，仅公钥下发客户端。

配套面板不是通用一键覆盖包。新面板部署或升级前，先备份站点、数据库和 manifest，并按该分支的 PHASE2_IMPLEMENTATION_REPORT.md、PHASE3_IMPLEMENTATION_REPORT.md、tools/admin-anytls/README.md 核对版本和前端哈希。已有本次修改的面板无需再次覆盖。

## 4. 创建节点服务

在 VPS 以 root 执行：

```bash
install -d -m 700 /etc/xboard-node-custom /var/lib/xboard-node-custom
install -d /opt/xboard-node-custom
install -m 755 /root/xboard-node.new /opt/xboard-node-custom/xboard-node
umask 077
cat > /etc/xboard-node-custom/config.yml <<'YAML'
panel:
  url: "https://panel.example.com"
machine:
  machine_id: 1
  token: "REPLACE_WITH_MACHINE_TOKEN"
kernel:
  type: singbox
log:
  level: info
health_port: 0
YAML
```

启动前，用编辑器把 URL、machine_id 和 token 改成真实值。URL 用面板根地址，不含管理路径或 `#/`。配置文件不上传 Git。

```bash
cat > /etc/systemd/system/xboard-node-custom.service <<'UNIT'
[Unit]
Description=Custom Xboard-Node
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/opt/xboard-node-custom/xboard-node -c /etc/xboard-node-custom/config.yml
WorkingDirectory=/var/lib/xboard-node-custom
Environment=GOMEMLIMIT=384MiB
Environment=GOGC=50
UMask=0077
Restart=on-failure
RestartSec=5
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
UNIT
systemctl daemon-reload
systemctl enable --now xboard-node-custom.service
systemctl status xboard-node-custom.service --no-pager
ss -lntup
```

384 MiB 是本次小内存测试的 Go 软内存目标，不是硬性资源隔离；更多用户/节点需要重新评估内存。

配置证书续期后的服务重启：

```bash
install -d /etc/letsencrypt/renewal-hooks/deploy
cat > /etc/letsencrypt/renewal-hooks/deploy/xboard-node-custom.sh <<'HOOK'
#!/bin/sh
systemctl try-restart xboard-node-custom.service
HOOK
chmod 700 /etc/letsencrypt/renewal-hooks/deploy/xboard-node-custom.sh
```

## 5. 订阅和验证

- FlClash 0.8.98：标准 AnyTLS 和 Hysteria2。AnyTLS REALITY 被有意过滤，因为 Mihomo 不支持这个组合：https://wiki.metacubex.one/config/proxies/anytls/
- AnyTLS REALITY：使用支持该组合的原生 sing-box 内核；本次实际使用仓库已有 cedar sing-box 依赖构建的测试客户端，不代表所有 sing-box 手机应用已验收。
- 订阅若显式设置 flag，FlClash 可用 `flag=flclash%2F0.8.98`，原生 sing-box 使用对应版本 flag。URL 无查询时用 `?`，已有查询时用 `&`；不要把完整订阅链接公开。
- 更新订阅，逐个选择节点，实际访问 HTTPS 页面/下载文件，再核对面板在线与流量；不能只看延迟数字。
- 检查 `journalctl -u xboard-node-custom -n 50 --no-pager`；分享日志前隐藏 token、用户 UUID、密钥。

## 6. 本次验证与待办

三种节点均完成出口 IP 检查、64 KiB HTTPS 下载（HTTP 200）及面板流量回报。用户另确认安卓 FlClash 的标准 AnyTLS 443 可用。在线 FlClash 和 sing-box 订阅均 HTTP 200。

部署中修复了 root 创建日志导致 Web 用户不能追加写入的 HTTP 500，并修复测试链接缺失版本导致 Hysteria2 被过滤。PHP 运维命令应使用站点运行用户；新日志应归属该用户，避免重现。

当前 WebSocket 出现 malformed URL，依靠 REST 轮询同步。下一阶段修复 WebSocket，并补齐 REST 断线重试、用户增删、配置更新、流量一致性等完整验收。限速、设备限制和热重载尚未完成全部实机验收。当前完整 sing-box 模板仍有旧 DNS 格式弃用警告，升级内核前需迁移验证。

## 7. 回退

新 VPS 隔离安装可先 `systemctl disable --now xboard-node-custom`，保留配置与日志排查。升级已有服务则恢复备份二进制、配置及 unit，再 daemon-reload 和重启。面板回退按备份清单还原 PHP 与 manifest；不要为撤销页面修改覆盖整个在线数据库。

## 2026-09-29 WebSocket 更新

主站 WebSocket URL 与宝塔反向代理已修复，设备 IP 列表去重后的 JSON 格式已兼容。两台服务器、四个节点已通过连接、主动 sync.nodes 推送和断线自动重连验证。上文未通过的描述是 9 月 27 日状态；当前 REST 仍保留作为补充。完整设备限制和长期故障验收尚未完成。面板分支的 WEBSOCKET_VERIFICATION.md 记录详细证据。
