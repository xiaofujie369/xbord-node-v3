# 全新 VPS 安装 xbord-node-v3（已测试版）

本指南教你在一台**全新的** Debian / Ubuntu VPS 上安装 xbord-node-v3 的已测试版本，并接入你的 XBoard 面板。所有命令都可以直接复制粘贴。

> 已经装过旧版本的机器，请看 [UPGRADE_GUIDE_ZH.md](UPGRADE_GUIDE_ZH.md)，不要用本指南重装。

## 0. 版本说明（先读）

| 项目 | 值 |
|---|---|
| GitHub 分支 | `release/xbord-node-v3-tested` |
| 程序代码提交 | `7dc66626bd7b4c4fa199716abafaba88be0639d1`（已推送到 GitHub，经过全部测试的版本） |
| 安装脚本 | 仓库内 `scripts/xbnode-v3.sh`，SHA-256 见第 3 步 |
| 获取方式 | 脚本在 VPS 上**按上面的固定提交从源码编译**。这个版本**还没有发布 GitHub Release 二进制包**，所以不能用旧的 `install-custom.sh`（它只会装旧版 `custom-hotreload-20261001`） |

脚本做的事情：安装依赖 → 下载官方 Go 1.26.10 并校验 SHA-256 → 从 GitHub 拉取固定提交的源码并核对提交号 → 编译 → 写入配置 → 创建 systemd 服务并设置开机自启 → 等待服务稳定运行。编译用的临时文件在结束后自动删除。

## 1. 系统要求

| 项目 | 要求 | 测试情况 |
|---|---|---|
| 系统 | Debian 12、Ubuntu 24.04 | ✅ 已在带真实 systemd 的隔离环境中完整测试 |
| 系统 | Debian 13、Ubuntu 22.04 | ⚠️ 未测试，理论上可用 |
| CPU 架构 | x86_64（amd64） | ✅ 已测试 |
| CPU 架构 | ARM64 | ⚠️ 脚本支持，但未测试 |
| 权限 | root | 必需 |
| 内存 | 编译时需要约 1.1 GB；内存+交换不足 2.6 GB 时，脚本会**自动创建 2 GB 临时交换文件**，编译完自动删除 | ✅ 已测试 |
| 硬盘 | 编译时至少 6 GB 可用空间（结束后释放） | 脚本会检查 |
| 网络 | 能访问 github.com、go.dev、proxy.golang.org | 必需 |
| 面板 | XBoard，并能用管理员账号登录后台 | 已用 XBoard（上游 `1f4e4e0`）实测 |

内存只有 512 MB、硬盘很小的机器，建议在另一台机器上编译，再把程序传过来，见第 5 步的「方法 B」。

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
echo "d707d6da3fa1b7260d1d95c12128aa73275f3865004406deab1bd326031422c3  /root/xbnode-v3.sh" | sha256sum -c -
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

### 方法 A：在本机编译并安装（推荐，最简单）

把下面三个值换成你自己的：

- `https://你的面板域名`：面板网址的根地址，**末尾不要加 `/`**。
- `--node-id 1`：第 2 步记下的节点 ID。
- `--node-type vless`：第 2 步记下的节点类型，用小写，可选值有 `vless`、`vmess`、`trojan`、`shadowsocks`、`hysteria`、`tuic`、`anytls`、`socks`、`naive`、`http`、`mieru`。

```bash
bash /root/xbnode-v3.sh install --panel-url https://你的面板域名 --node-id 1 --node-type vless
```

脚本会提示：`请粘贴面板「系统配置 → 节点配置 → 通讯密钥」（输入不显示）`。粘贴第 2.1 步的通讯密钥，按回车。**输入时屏幕上什么都不显示，这是正常的。**

整个过程大约 10 到 20 分钟，大部分时间在编译。看到下面这样的输出就是成功了：

```
==> 安装完成并已设置开机自启：xboard-node v3-tested-7dc6662 (built 2026-10-20T20:21:39Z)
```

**可选：核对编译结果。** 这个版本的编译是可复现的：同一个提交、同一个 Go 版本，在任何 x86_64 机器上编译出的 `xboard-node` 都完全相同。编译完成时脚本会显示 `sha256=...`，应该等于：

```
39ce3a3b292a4dc0a51c9d50c98844af89b65847a4e5aa10e2c8d931538c2914
```

### 方法 B：小内存 VPS，在另一台机器上编译后传过来

在一台内存充足的 Debian/Ubuntu 机器上（以 root 身份）执行：

```bash
curl -fL -o /root/xbnode-v3.sh https://raw.githubusercontent.com/xiaofujie369/xbord-node-v3/release/xbord-node-v3-tested/scripts/xbnode-v3.sh
bash /root/xbnode-v3.sh build
scp /root/xbnode-v3-build/xboard-node root@新VPS的IP:/root/xboard-node-v3
```

再回到新 VPS 上安装，用 `--binary` 指定传过来的程序：

```bash
bash /root/xbnode-v3.sh install --panel-url https://你的面板域名 --node-id 1 --node-type vless --binary /root/xboard-node-v3
```

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
| 编译时提示内存或磁盘不足 | 改用第 5 步的方法 B |

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
| 全新安装：自动装依赖、下载并校验 Go、拉取固定提交并核对、源码编译、写配置、创建服务和开机自启 | Debian 12 最小化系统（隔离环境） | ✅ 通过，耗时约 8 分钟 |
| 安装后：用户认证、拒绝未知用户、TCP、UDP、SSRF 拦截、流量记账（500 万 TCP + 10 万 UDP 与面板记录一致） | Debian 12（隔离环境） | ✅ 通过 |
| 重启机器后服务自动启动 | Debian 12（隔离环境） | ✅ 约 2 秒恢复 |
| 从旧版 `custom-hotreload-20261001`（用 `install-custom.sh` 的布局）升级，配置文件完全不变 | Debian 12（隔离环境） | ✅ 通过 |
| 升级中断时长 | Debian 12（隔离环境） | ✅ 实测约 0.4 秒 |
| 手动回滚、新版本启动即崩溃时自动回滚 | Debian 12（隔离环境） | ✅ 通过 |
| 从上游 v1.13（用真实 `xbctl config init` 生成的 `install.sh` 标准布局）源码编译升级，配置和 `credentials.env` 不变，`xbctl` 一并更新，编译文件自动清理，回滚 | Ubuntu 24.04（隔离环境） | ✅ 通过 |
| 交互输入通讯密钥：屏幕、系统日志中都不出现密钥，只保存在权限 600 的配置文件里 | Ubuntu 24.04（隔离环境） | ✅ 通过 |
| 方法 B：`build` 编译 + `--binary` 安装或升级 | 开发 VPS + Ubuntu 24.04 / Debian 12（隔离环境） | ✅ 通过 |
| 内存不足时自动创建并删除 2 GB 临时交换文件 | 开发 VPS（Ubuntu 24.04，通过调高阈值强制触发） | ✅ 通过 |
| 可复现编译：两台机器分别编译，SHA-256 完全一致 | 开发 VPS 与 Ubuntu 24.04 容器 | ✅ 通过 |
| 错误输入（面板地址格式、节点 ID、节点类型、已安装/未安装、无备份时回滚等）被拒绝，且不会开始编译 | Ubuntu 24.04（隔离环境） | ✅ 通过 |
| 卸载：`uninstall --purge`，两种布局 | Debian 12 / Ubuntu 24.04（隔离环境） | ✅ 通过 |
| **用本脚本安装后对接真实 XBoard 面板**：WebSocket 连接、用户同步、认证、TCP、UDP，面板记账 3,100,117 字节，与实际流量 3,100,000 字节吻合 | 真实 XBoard（上游 `1f4e4e0`），测试节点和用户已删除 | ✅ 通过 |
| Debian 13、Ubuntu 22.04 | — | ⚠️ 未测试 |
| ARM64 架构 | — | ⚠️ 未测试 |
| 真实云 VPS 整机安装（以上都在容器中完成） | — | ⚠️ 未测试 |
| 需要 TLS 证书的节点（Trojan、Hysteria2、TUIC、VLESS+TLS 等） | — | ⚠️ 本轮未测试 |
| machine 模式（机器 ID + 机器 token）的配置 | — | ⚠️ 未测试；升级不改配置，理论上不受影响 |
| xray 内核的真实客户端 | — | ⚠️ 未测试 |
| ufw 防火墙命令 | — | ⚠️ 为 ufw 标准用法，本轮未执行 |
| Docker 方式部署的节点 | — | ❌ 不支持，本指南不适用 |
