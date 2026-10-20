# 把已有 VPS 上的 xbord-node 升级到 v3 已测试版

本指南教你把一台**已经在运行**的 xbord-node 节点，安全地升级到 xbord-node-v3 的已测试版本，并且尽量不中断用户。所有命令都可以直接复制粘贴。

> 新买的 VPS 请看 [FRESH_INSTALL_GUIDE_ZH.md](FRESH_INSTALL_GUIDE_ZH.md)。

## 0. 版本说明（先读）

| 项目 | 值 |
|---|---|
| GitHub 分支 | `release/xbord-node-v3-tested` |
| 程序代码提交 | `7dc66626bd7b4c4fa199716abafaba88be0639d1`（已推送到 GitHub，经过全部测试的版本） |
| 升级脚本 | 仓库内 `scripts/xbnode-v3.sh`，SHA-256 见第 3 步 |
| 获取方式 | 按上面的固定提交**从源码编译**。这个版本还没有 GitHub Release 二进制包，所以**旧的 `install-custom.sh` / `upgrade-custom.sh` 不能用来升级到这个版本**（它们只会装旧版 `custom-hotreload-20261001`）；上游的 `xbctl upgrade` 也不行，它会装上游版本，你的定制修改会丢失 |

**升级会保留什么**：面板地址、通讯密钥、节点 ID / 机器 ID、证书路径等所有配置都原样保留，脚本**不会改动配置文件**。用户数据存放在 XBoard 面板上，节点本地没有数据库，不受影响。

**停机时间**：编译阶段旧节点照常服务，只有最后替换程序、重启服务这一下会短暂中断。隔离环境实测：升级期间每 0.2 秒探测一次，183 次中只有 2 次失败，**中断约 0.4 秒**。正在进行的连接会断开，客户端会自动重连。

## 1. 判断现在的安装方式和版本

用 SSH 登录 VPS（`ssh root@你的VPS的IP`），然后执行：

```bash
systemctl list-units --all --no-pager | grep -E 'xboard-node' ; ls -l /opt/xboard-node-custom/xboard-node /usr/local/bin/xboard-node 2>/dev/null
```

对照下表：

| 看到的结果 | 安装方式 | 本指南能否升级 |
|---|---|---|
| 服务 `xboard-node-custom.service`，程序 `/opt/xboard-node-custom/xboard-node` | 本仓库的 `install-custom.sh`（定制布局） | ✅ 可以（已测试） |
| 服务 `xboard-node.service`，程序 `/usr/local/bin/xboard-node` | `install.sh` / 上游安装方式（标准布局） | ✅ 可以（已测试） |
| 都没有，或者用 Docker 运行 | 其他方式 | ❌ 本指南不适用。Docker 部署未测试 |

查看当前版本：

```bash
/opt/xboard-node-custom/xboard-node -v 2>/dev/null; /usr/local/bin/xboard-node -v 2>/dev/null
```

## 2. 检查资源

```bash
free -m
df -h /root
```

- 编译时需要内存+交换合计约 2.6 GB。不够时，脚本会**自动创建 2 GB 临时交换文件**，用完自动删除。
- `/root` 所在分区要有至少 6 GB 可用空间，编译完会释放。
- 如果内存只有 512 MB，或者硬盘很小，请用第 5 步的「方法 B」，在别的机器上编译。

## 3. 下载升级脚本并校验

```bash
apt-get update && apt-get install -y curl ca-certificates
curl -fL -o /root/xbnode-v3.sh https://raw.githubusercontent.com/xiaofujie369/xbord-node-v3/release/xbord-node-v3-tested/scripts/xbnode-v3.sh
echo "d707d6da3fa1b7260d1d95c12128aa73275f3865004406deab1bd326031422c3  /root/xbnode-v3.sh" | sha256sum -c -
```

最后一行必须输出 `/root/xbnode-v3.sh: OK`。如果显示 `FAILED`，**停止操作**，脚本可能被篡改了，或者下载不完整。

查看脚本识别到的当前安装：

```bash
bash /root/xbnode-v3.sh status
```

## 4. 手动再做一份备份（建议）

脚本升级时会自动备份，但多一份备份更安心。下面的命令会把程序、配置（含通讯密钥）、服务文件打包，保存到 `/root/xbnode-manual-backup-日期.tar.gz`：

```bash
tar -czf /root/xbnode-manual-backup-$(date +%Y%m%d-%H%M%S).tar.gz \
  $(ls -d /opt/xboard-node-custom /etc/xboard-node-custom /etc/systemd/system/xboard-node-custom.service \
          /usr/local/bin/xboard-node /usr/local/bin/xbctl /etc/xboard-node /etc/systemd/system/xboard-node.service 2>/dev/null)
chmod 600 /root/xbnode-manual-backup-*.tar.gz
ls -lh /root/xbnode-manual-backup-*.tar.gz
```

执行时如果出现 `tar: Removing leading '/' from member names`，这是正常提示，不是错误。

如果节点使用 Let's Encrypt 证书，再多备份一份证书目录：

```bash
[ -d /etc/letsencrypt ] && tar -czf /root/letsencrypt-backup-$(date +%Y%m%d).tar.gz /etc/letsencrypt && chmod 600 /root/letsencrypt-backup-*.tar.gz
```

> 备份里有通讯密钥或机器 token，**不要发给任何人**，也不要上传到公开的地方。

## 5. 升级

### 方法 A：在本机编译并升级（推荐）

```bash
bash /root/xbnode-v3.sh upgrade
```

脚本会依次执行：

1. 识别安装方式，显示当前版本。
2. 下载 Go 并编译新版本。这段时间**旧节点照常服务**，大约 10 到 20 分钟。
3. 自动备份程序、配置目录和服务文件到 `/root/xbnode-v3-backup-日期-xxxx/`。
4. 替换程序，重启服务（实测中断约 0.4 秒）。
5. 等服务稳定运行 8 秒，确认成功。**如果新版本没能稳定运行，会自动回滚到旧版本。**

看到下面的输出就是升级成功了：

```
==> 升级完成：xboard-node v3-tested-7dc6662 (built 2026-10-20T20:21:39Z)。备份保留在 /root/xbnode-v3-backup-...
```

**可选：核对编译结果。** 这个版本的编译是可复现的：同一个提交、同一个 Go 版本，在任何 x86_64 机器上编译出的 `xboard-node` 都完全相同。编译完成时脚本会显示 `sha256=...`，应该等于：

```
39ce3a3b292a4dc0a51c9d50c98844af89b65847a4e5aa10e2c8d931538c2914
```

### 方法 B：小内存 VPS，在另一台机器上编译后传过来

在一台内存充足的 Debian/Ubuntu 机器上（root 身份）：

```bash
curl -fL -o /root/xbnode-v3.sh https://raw.githubusercontent.com/xiaofujie369/xbord-node-v3/release/xbord-node-v3-tested/scripts/xbnode-v3.sh
bash /root/xbnode-v3.sh build
scp /root/xbnode-v3-build/xboard-node root@要升级的VPS的IP:/root/xboard-node-v3
```

再到要升级的 VPS 上：

```bash
bash /root/xbnode-v3.sh upgrade --binary /root/xboard-node-v3
```

> 标准布局（`xboard-node.service`）用方法 B 时，只会替换节点程序，`xbctl` 命令保持原样。

## 6. 验证升级结果

**6.1 版本和服务状态**：版本应该是 `v3-tested-7dc6662`，服务应该是 `active`：

```bash
bash /root/xbnode-v3.sh status
```

**6.2 日志**（按 `Ctrl+C` 退出）：

```bash
journalctl -u xboard-node-custom -n 50 --no-pager   # 定制布局
journalctl -u xboard-node -n 50 --no-pager          # 标准布局
```

应该能看到 `started, N users`。面板开启了 WebSocket 时，还会看到 `ws connected`。

**6.3 面板**：在 XBoard「节点管理」里，节点应该保持在线。

**6.4 客户端**：用客户端连接这个节点，确认能上网，UDP 也正常。

**6.5 流量**：下载一个已知大小的文件，1 到 2 分钟后在后台查看该用户的已用流量是否相应增加。

> 新版本修复了旧版 **UDP 下载流量没有计费**的问题。升级后用户的已用流量可能比以前增长得快一些，这是修复后的正确结果。

## 7. 回滚到旧版本

升级后如果发现问题，一条命令就能回到升级前的版本：

```bash
bash /root/xbnode-v3.sh rollback
```

它会使用最近一次的备份。如果要指定某一次备份：

```bash
ls -d /root/xbnode-v3-backup-*
bash /root/xbnode-v3.sh rollback /root/xbnode-v3-backup-你要的那一个
```

**手动回滚**（脚本也不能用时），以定制布局为例：

```bash
B=$(ls -1dt /root/xbnode-v3-backup-* | head -1)
install -m 755 "$B/xboard-node" /opt/xboard-node-custom/xboard-node
systemctl restart xboard-node-custom
systemctl status xboard-node-custom --no-pager
```

标准布局把路径换成 `/usr/local/bin/xboard-node`，服务名换成 `xboard-node`。

## 8. 常见问题排查

| 现象 | 处理 |
|---|---|
| 脚本提示「没有找到已安装的节点」 | 你的安装不属于第 1 步表格里的两种布局，本指南不适用 |
| 提示「同时检测到两种安装」 | 这台机器上同时有两套节点，需要先确认哪一套在用，手动停用另一套 |
| 编译时提示磁盘不足或内存不足 | 改用第 5 步的方法 B |
| 显示「已回滚到原版本并恢复运行」 | 新版本没能稳定运行。用 `journalctl -u 服务名 -n 100 --no-pager` 查看原因，旧版本已经恢复服务 |
| 日志里有 `handshake status 403` | 通讯密钥不对。升级不会改配置，请检查面板上的通讯密钥最近是否改过 |
| 升级后客户端连不上 | 先执行回滚确认问题是否和新版本有关，再查看日志 |

## 9. 清理

确认新版本运行正常几天后，可以删除旧备份来释放空间：

```bash
ls -d /root/xbnode-v3-backup-* /root/xbnode-manual-backup-*.tar.gz
rm -rf /root/xbnode-v3-backup-要删除的那一个
```

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
