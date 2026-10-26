# 全新 VPS 安装 xbord-node-v3（已测试版）

本指南教你在一台**全新的** Debian / Ubuntu VPS 上安装 xbord-node-v3 的已测试版本，并接入你的 XBoard 面板。所有命令都可以直接复制粘贴。

> 已经装过旧版本的机器，请看 [UPGRADE_GUIDE_ZH.md](UPGRADE_GUIDE_ZH.md)，不要用本指南重装。

## 0. 版本说明（先读）

| 项目 | 值 |
|---|---|
| GitHub 分支 | `release/xbord-node-v3-tested` |
| 程序版本 | GitHub Release [`v3.0.0`](https://github.com/xiaofujie369/xbord-node-v3/releases/tag/v3.0.0)，程序代码与经过全部测试的提交 `7dc66626bd7b4c4fa199716abafaba88be0639d1` 完全相同 |
| 编译方式 | 由 **GitHub Actions 自动编译**（编译前先跑完全部单元测试），VPS 上**不编译**，只下载程序，几十秒完成，小内存 VPS 也能用 |
| 安装脚本 | 仓库内 `scripts/xbnode-v3.sh`，SHA-256 见第 3 步 |
| 注意 | 不要用旧的 `install-custom.sh`，它只会装旧版 `custom-hotreload-20261001` |

脚本做的事情：从 GitHub Release 下载 v3.0.0 程序 → 用脚本里写死的 SHA-256 校验（不符就停止）→ 写入配置 → 创建 systemd 服务并设置开机自启 → 等待服务稳定运行。

## 一键安装命令（熟悉的话直接用）

先在面板上准备好节点（第 2 步），然后把命令里的面板地址、节点 ID、节点类型换成你自己的：

```bash
apt-get update && apt-get install -y curl ca-certificates && curl -fsSL -o /root/xbnode-v3.sh https://raw.githubusercontent.com/xiaofujie369/xbord-node-v3/release/xbord-node-v3-tested/scripts/xbnode-v3.sh && echo "cfa4faedaf0a6de45f6b9097e8835897fb9204aed96f75efd20d493db8fdbdc4  /root/xbnode-v3.sh" | sha256sum -c - && bash /root/xbnode-v3.sh install --panel-url https://你的面板域名 --node-id 1 --node-type vless
```

脚本会提示粘贴通讯密钥（输入不显示）。下面是分步说明。

## 1. 系统要求

| 项目 | 要求 | 测试情况 |
|---|---|---|
| 系统 | Debian 12、Ubuntu 24.04 | ✅ 已在带真实 systemd 的隔离环境中完整测试 |
| 系统 | Debian 13、Ubuntu 22.04 | ⚠️ 未测试，理论上可用 |
| CPU 架构 | x86_64（amd64） | ✅ 已测试 |
| CPU 架构 | ARM64 | ⚠️ 脚本支持，但未测试 |
| 权限 | root | 必需 |
| 内存 | 不编译，只运行节点；隔离环境在 600 MB 内存限制下完成安装和测试 | ✅ 已测试 |
| 硬盘 | 约 150 MB 可用空间 | — |
| 网络 | 能访问 github.com、raw.githubusercontent.com | 必需 |
| 面板 | XBoard，并能用管理员账号登录后台 | 已用 XBoard（上游 `1f4e4e0`）实测 |

## 2. 在 XBoard 后台准备节点

用管理员账号登录 XBoard 后台。

**2.1 记下通讯密钥**

进入「系统配置 → 节点配置 → 通讯密钥」，复制里面的内容。这就是节点连接面板用的密码，**不要发给别人**。如果是空的，自己设置一个长随机字符串并保存。

**2.2 添加节点**

进入「节点管理 → 添加节点」，选择协议类型（例如 VLESS、Trojan、Shadowsocks、Hysteria、TUIC、AnyTLS、SOCKS 等）：

- **节点地址**：填这台新 VPS 的公网 IP 或域名（客户端用它来连接）。
- **连接端口 / 服务端口**：例如 `443`。节点程序会在**服务端口**上监听。
- **权限组**：选择允许使用这个节点的用户所在的权限组。
- 其他协议参数（TLS、传输方式等）按你的需要填写。

保存后，在节点列表里记下这个节点的 **ID**（数字）和**类型**。

> 需要 TLS 证书的节点（例如 Trojan、Hysteria2、TUIC、带 TLS 的 VLESS/AnyTLS）：本次真实测试使用的是不需要证书的 SOCKS 节点，**证书相关配置本次未重新测试**。证书的配置方式请参考 [SETUP_RENEWAL_ZH.md](SETUP_RENEWAL_ZH.md) 和 [DEPLOYMENT_ZH.md](DEPLOYMENT_ZH.md)。

## 3. 登录 VPS 并下载安装脚本

用 SSH 登录新 VPS（Windows 可以用 PowerShell、Termius 或 FinalShell）：

```bash
ssh root@你的VPS的IP
```

先安装下载工具（全新系统可能没有 curl）：

```bash
apt-get update && apt-get install -y curl ca-certificates
```

下载安装脚本：

```bash
curl -fL -o /root/xbnode-v3.sh https://raw.githubusercontent.com/xiaofujie369/xbord-node-v3/release/xbord-node-v3-tested/scripts/xbnode-v3.sh
```

校验脚本没有被篡改（输出 `OK` 才能继续；如果显示 `FAILED`，请停止并联系维护者）：

```bash
echo "cfa4faedaf0a6de45f6b9097e8835897fb9204aed96f75efd20d493db8fdbdc4  /root/xbnode-v3.sh" | sha256sum -c -
```

## 4. 放行防火墙端口

节点要监听面板里设置的**服务端口**。客户端要能从外网连进来，需要两处都放行：

**4.1 云服务商的安全组 / 防火墙**（在服务商网页后台操作）：放行这个端口的 **TCP 和 UDP**。很多协议都要用到 UDP，比如 Hysteria、TUIC 只走 UDP，SOCKS 和 Shadowsocks 也会用 UDP。

**4.2 系统防火墙**：先看是否启用了 ufw：

```bash
ufw status
```

- 显示 `Status: inactive` 或提示命令不存在：不用处理。
- 显示 `Status: active`：执行下面的命令，把 `443` 换成你的服务端口。**一定要先放行 SSH 端口**，否则可能把自己锁在外面：

```bash
ufw allow 22/tcp
ufw allow 443/tcp
ufw allow 443/udp
ufw status
```

## 5. 安装

把下面三个值换成你自己的：

- `https://你的面板域名`：面板网址的根地址，**末尾不要加 `/`**。
- `--node-id 1`：第 2 步记下的节点 ID。
- `--node-type vless`：第 2 步记下的节点类型，用小写，可选值有 `vless`、`vmess`、`trojan`、`shadowsocks`、`hysteria`、`tuic`、`anytls`、`socks`、`naive`、`http`、`mieru`。

```bash
bash /root/xbnode-v3.sh install --panel-url https://你的面板域名 --node-id 1 --node-type vless
```

脚本会提示：`请粘贴面板「系统配置 → 节点配置 → 通讯密钥」（输入不显示）`。粘贴第 2.1 步的通讯密钥，按回车。**输入时屏幕上什么都不显示，这是正常的。**

一般不到 1 分钟就能完成。看到下面这样的输出就是成功了：

```
==> 安装完成并已设置开机自启：xboard-node v3.0.0 (built 2026-10-21T01:35:02Z)
```

> 无法访问 GitHub 的机器：在能访问的电脑上从 [Release 页面](https://github.com/xiaofujie369/xbord-node-v3/releases/tag/v3.0.0) 下载 `xboard-node-linux-amd64`，传到 VPS 的 `/root/`，安装命令末尾加上 `--binary /root/xboard-node-linux-amd64`。

## 多端口：用户随机连接端口（可选）

效果：面板给每个用户的订阅随机分配一个范围内的端口（例如 8443-11443），所有这些端口都能连到同一个节点。

**原理**：XBoard 下发给节点的「服务端口」只能是一个数字，节点只监听这一个端口。脚本用系统防火墙（nftables）把整个范围的 TCP 和 UDP 都转到这个端口上，IPv4 和 IPv6 都生效，开机自动恢复。转发规则放在脚本自己的规则表里，不会改动你原有的防火墙规则。

**1. 面板设置**（「节点管理」→ 编辑节点）：

- **连接端口**：填范围，例如 `8443-11443`。用户每次更新订阅时，会随机拿到其中一个端口。
- **服务端口**：只填**一个**端口，例如 `8443`。不要填范围，填了也只会用第一个端口。

**2. 在节点 VPS 上开启转发**（把范围和端口换成你自己的）：

```bash
bash /root/xbnode-v3.sh port-range 8443-11443 --listen-port 8443
```

`--listen-port` 要与面板的「服务端口」一致；不写时默认是范围的第一个端口。全新安装时，也可以直接在 `install` 命令末尾加上 `--port-range 8443-11443 --listen-port 8443`。

**3. 云服务商安全组**：放行整个范围 `8443-11443` 的 **TCP 和 UDP**。系统里的 ufw 只需要放行服务端口本身（例如 `8443`），因为转发发生在 ufw 检查之前。

**查看、修改、关闭：**

```bash
bash /root/xbnode-v3.sh status                                   # 显示「端口范围：8443-11443 → 8443（active）」
bash /root/xbnode-v3.sh port-range 9000-9999 --listen-port 8443  # 换一个范围（直接覆盖旧的）
bash /root/xbnode-v3.sh port-range off                           # 关闭转发
```

注意事项：

- 范围内如果已经有其他程序在监听（例如 SSH、网站），脚本会拒绝开启，避免把它们的连接转走。
- 升级、回滚不会影响转发；卸载节点时会一并删除转发规则。
- 如果你自己执行了 `systemctl restart nftables`，它会清空所有规则，之后执行 `systemctl restart xbnode-port-range` 恢复转发。
- 订阅里的端口是**每次更新订阅时随机一次**，不是每次连接都换。Hysteria2 节点的订阅还会带上整个范围，支持的客户端会自动在这些端口之间跳。

## 6. 检查是否安装成功

**6.1 服务状态**（应显示 `active (running)` 和 `enabled`）：

```bash
bash /root/xbnode-v3.sh status
systemctl status xboard-node-custom --no-pager
```

**6.2 查看日志**（按 `Ctrl+C` 退出）：

```bash
journalctl -u xboard-node-custom -f
```

正常的日志里会看到类似这样的内容：

- `ws connected`：已经和面板建立实时连接。如果面板没有开启 WebSocket，就看不到这一行，这也正常。
- `started, N users`：节点已启动，从面板拉到了 N 个用户。

**6.3 端口是否在监听**（把 443 换成你的端口）：

```bash
ss -tulnp | grep 443
```

**6.4 面板上看节点状态**：回到 XBoard「节点管理」，过 1 到 2 分钟，这个节点应该显示为在线。

**6.5 用客户端实际测试**

1. 用一个属于该权限组的用户，在客户端里更新订阅。
2. 选择这个节点，打开任意网页，确认能访问。这一步验证的是**认证和 TCP**。
3. **UDP**：用支持 UDP 的客户端，开启 UDP 转发后测试，例如访问网页时使用 QUIC/HTTP3，或使用游戏、语音通话。Hysteria 和 TUIC 节点本身就是走 UDP 的。
4. **流量记账**：用客户端下载一个已知大小的文件，比如 100 MB。等 1 到 2 分钟后，在 XBoard 后台查看该用户的已用流量，应该增加约 100 MB 乘以节点倍率。

## 7. 常见问题排查

| 现象 | 可能原因和处理 |
|---|---|
| 日志里有 `handshake status 403` 或 `token is error` | 通讯密钥填错了。修改 `/etc/xboard-node-custom/config.yml` 里的 `token`，再执行 `systemctl restart xboard-node-custom` |
| 日志里有 `Server does not exist` | 节点 ID 或类型和面板不一致。修改配置里的 `node_id` 和 `node_type`，然后重启 |
| 日志里一直没有 `started` | 面板上这个节点的权限组里还没有可用用户。至少要有一个未过期、未封禁、流量未用完的用户 |
| 客户端连不上，日志正常 | 第 4 步的防火墙或安全组没放行，或者客户端用的端口和服务端口不一致 |
| 提示「下载失败」或「SHA-256 校验失败」 | 网络无法访问 GitHub 或下载不完整，稍后重试；仍失败就按第 5 步的提示手动下载后用 `--binary` |

修改配置文件可以用 nano 编辑器：执行 `nano /etc/xboard-node-custom/config.yml`，改完按 `Ctrl+O` 回车保存，再按 `Ctrl+X` 退出。

## 8. 以后如何更新

按 [UPGRADE_GUIDE_ZH.md](UPGRADE_GUIDE_ZH.md) 操作。核心命令是：

```bash
bash /root/xbnode-v3.sh upgrade
```

## 9. 卸载

停止服务并删除程序，**保留**配置（以后重装时可以参考）：

```bash
bash /root/xbnode-v3.sh uninstall
```

彻底删除，连同配置和通讯密钥一起删除：

```bash
bash /root/xbnode-v3.sh uninstall --purge
```

卸载后，记得在 XBoard 后台删除或停用这个节点，并在防火墙里关闭对应端口。

## 10. 哪些已经测试、哪些没有

测试日期 2026-10-21。「隔离环境」指带**真实 systemd** 的 Docker 容器（服务的启动、开机自启、重启都是真实执行的），节点连接的是按 XBoard 接口实现的模拟面板。

| 测试项 | 环境 | 结果 |
|---|---|---|
| **v3.0.0 Release 全新安装**：从 GitHub 下载、校验 SHA-256、写配置、创建服务并开机自启，VPS 上没有安装 Go、没有编译 | Debian 12（隔离环境，内存限制 600 MB） | ✅ 通过，耗时 26 秒 |
| **v3.0.0 Release 升级**：从旧版 `custom-hotreload-20261001`（`install-custom.sh` 布局）升级，配置不变；随后回滚 | Debian 12（隔离环境，内存限制 600 MB） | ✅ 通过，升级耗时 12 秒 |
| **v3.0.0 Release 升级**：从上游 v1.13（`install.sh` 标准布局）升级，配置和 `credentials.env` 不变，`xbctl` 一并更新为 v3.0.0；随后回滚 | Ubuntu 24.04（隔离环境，内存限制 600 MB） | ✅ 通过，耗时约 12 秒 |
| 下载的文件 SHA-256 不符时拒绝安装，旧节点不受影响 | Debian 12（隔离环境） | ✅ 通过 |
| **多端口转发**（`port-range` / `install --port-range`）：范围内的端口 TCP 都能连到节点，范围外的端口不转发；UDP 转发到节点端口；`status` 显示；开机自启；重启后规则自动恢复；范围内有其他程序监听时拒绝；更换范围；`off` 关闭；卸载时清除；错误参数被拒绝且不会安装 | Debian 12（隔离环境，独立网络命名空间） | ✅ 12/12 通过 |
| GitHub Actions：单元测试、amd64 / arm64 编译、生成 SHA256SUMS、发布 Release | GitHub | ✅ 全部成功 |

下面几项是用同一份程序代码（提交 `7dc6662`）在本地编译的版本测的，功能代码与 v3.0.0 完全相同，只是版本号显示不同：

| 测试项 | 环境 | 结果 |
|---|---|---|
| 安装后：用户认证、拒绝未知用户、TCP、UDP、SSRF 拦截、流量记账（500 万 TCP + 10 万 UDP 与面板记录一致） | Debian 12（隔离环境） | ✅ 通过 |
| 重启机器后服务自动启动 | Debian 12（隔离环境） | ✅ 约 2 秒恢复 |
| 从旧版 `custom-hotreload-20261001`（用 `install-custom.sh` 的布局）升级，配置文件完全不变 | Debian 12（隔离环境） | ✅ 通过 |
| 升级中断时长 | Debian 12（隔离环境） | ✅ 实测约 0.4 秒 |
| 手动回滚、新版本启动即崩溃时自动回滚 | Debian 12（隔离环境） | ✅ 通过 |
| 交互输入通讯密钥：屏幕、系统日志中都不出现密钥，只保存在权限 600 的配置文件里 | Ubuntu 24.04（隔离环境） | ✅ 通过 |
| 错误输入（面板地址格式、节点 ID、节点类型、已安装/未安装、无备份时回滚等）被拒绝 | Ubuntu 24.04（隔离环境） | ✅ 通过 |
| 卸载：`uninstall --purge`，两种布局 | Debian 12 / Ubuntu 24.04（隔离环境） | ✅ 通过 |
| **用本脚本安装后对接真实 XBoard 面板**：WebSocket 连接、用户同步、认证、TCP、UDP，面板记账 3,100,117 字节，与实际流量 3,100,000 字节吻合 | 真实 XBoard（上游 `1f4e4e0`），测试节点和用户已删除 | ✅ 通过 |
| Debian 13、Ubuntu 22.04 | — | ⚠️ 未测试 |
| ARM64 架构 | — | ⚠️ 程序已由 GitHub 编译并发布，未在 ARM64 机器上实际运行 |
| 真实云 VPS 整机安装（以上都在容器中完成） | — | ⚠️ 未测试 |
| 需要 TLS 证书的节点（Trojan、Hysteria2、TUIC、VLESS+TLS 等） | — | ⚠️ 本轮未测试 |
| machine 模式（机器 ID + 机器 token）的配置 | — | ⚠️ 未测试；升级不改配置，理论上不受影响 |
| xray 内核的真实客户端 | — | ⚠️ 未测试 |
| ufw 防火墙命令 | — | ⚠️ 为 ufw 标准用法，本轮未执行 |
| 多端口：用真实 Hysteria2 / TUIC / VLESS Reality 客户端经范围内端口连接；IPv6 转发 | — | ⚠️ 未测试（已验证转发规则本身对 TCP 和 UDP 有效） |
| Docker 方式部署的节点 | — | ❌ 不支持，本指南不适用 |
