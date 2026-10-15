# 一体安装与证书续期（2026-10-16）

统一入口是 `install-custom.sh`，无需安装后再执行 `upgrade-custom.sh`。
当前安装与升级均使用经过测试的正式发行包 `custom-hotreload-20261001`，
不是每次从 main 编译未发布的实验代码。

```bash
apt-get update && apt-get install -y ca-certificates curl && curl -fL --retry 3 https://raw.githubusercontent.com/xiaofujie369/xbord-node-v3/main/install-custom.sh -o /root/install-xboard-custom.sh && bash /root/install-xboard-custom.sh
```

## 新 VPS

适用 Debian/Ubuntu amd64，root 和 systemd。输入面板根地址、服务器 ID、专用 token。
需要证书时输入域名、邮箱并同意签发条款。域名应直接解析到本机，TCP 80 可访问且空闲。
脚本安装程序、写入证书路径、启用开机启动和 Certbot 定时任务，并进行模拟续期。
节点端口按面板实际设置放行；脚本不改防火墙。

新装 Go 软内存目标为物理内存的三分之一，上限 1024 MiB；它不是进程总内存硬限制。
已有服务的内存配置不会被覆盖。

## 已有标准安装

识别 `/opt/xboard-node-custom/xboard-node`、`/etc/xboard-node-custom/config.yml`
和对应 systemd unit。先备份配置、unit 和完整 `/etc/letsencrypt` 到
`/root/xboard-node-setup-*`（目录仅 root 可读）。程序校验和一致则跳过升级；
不一致则调用固定提交、SHA-256 校验过的升级器，保留其程序备份和失败回退机制。
不覆盖现有 token、面板地址或证书路径。残缺安装、Docker、其他服务名不会自动迁移。

## 证书路径和续期

始终使用下面两个路径，不要填 `archive/.../fullchain1.pem`：

```text
/etc/letsencrypt/live/你的证书名/fullchain.pem
/etc/letsencrypt/live/你的证书名/privkey.pem
```

Certbot 更新 `live/` 下的软链接，配置中的路径保持不变。多个节点使用同一证书时，
各节点的客户端 SNI 必须在证书覆盖的域名范围内；证书不会自动覆盖其他域名。

脚本从本地全局及 instances 的 file 证书设置收集 Certbot 路径，写入
`/etc/xboard-node-custom/certbot-lineages`。面板另行下发、但不在本地配置内的证书
不自动纳入此列表。外部签发工具管理的证书保留原续期方式。

部署钩子 `/etc/letsencrypt/renewal-hooks/deploy/xboard-node-custom.sh`
仅响应列表中的证书，检查有效期和公私钥匹配后执行 `systemctl try-restart`。
它不会启动被管理员停止的服务。成功续期会短暂断开本进程下的节点连接。

安装最后执行针对已配置证书的 `certbot renew --dry-run --run-deploy-hooks`，
测试 HTTP 验证和部署钩子，演练不会替换正式证书。Certbot 锁冲突最多等待约两分钟，
其他错误立即返回失败，不会把失败报告成完成。节点可能已经运行；修复 DNS/80 端口等
问题后可直接再次执行同一个入口。已有证书仍沿用其保存的验证方式。

检查命令：

```bash
systemctl status xboard-node-custom --no-pager
systemctl list-timers certbot.timer --all
journalctl -u certbot.service --no-pager -n 30
```

## 验证

自动化覆盖首次安装、重复执行不重复下载、按内存配置、新旧配置保留、证书路径、
正确证书续期钩子、无关证书不重启、公私钥不匹配拒绝重启、缺失私钥、无证书、
续期失败及 Certbot 锁冲突重试。升级器原有校验失败和启动失败回退测试继续通过。

```bash
python3 tests/install_cert_config_test.py
python3 tests/setup_install_test.py
python3 tests/setup_renewal_test.py
python3 tests/upgrade_custom_test.py
```

这些脚本在隔离临时路径和模拟系统命令下运行，需要 Linux、Bash、Python/PyYAML、
OpenSSL 和 util-linux。服务存活和端口监听不等于每个客户端已成功访问外网，
安装后仍需更新订阅测试实际节点连接。

2026-10-16 实机验收：Debian 13 已有标准安装中完成配置/证书备份，识别当前二进制
后跳过重复升级，Certbot 模拟续期及部署钩子成功；原配置逐字节保持一致，
4 个节点恢复监听，WebSocket mux 启动。该 VPS 已有安装，首次安装流程通过隔离
自动化测试验证；本次没有将生产 VPS 清空重装，也没有逐个客户端做端到端测速。
