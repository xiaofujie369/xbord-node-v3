# xboard-node

Node backend for [Xboard](https://github.com/cedar2025/Xboard). Supports `sing-box` / `xray-core` dual kernels.

> **Disclaimer**: This project is for educational and learning purposes only.

## Features

- Protocols: V2Ray family, Trojan, Shadowsocks, Hysteria2, TUIC, AnyTLS
- Sync: WebSocket push + REST polling dual channel
- User controls: speed limit, device limit, alive-IP tracking, hot update
- Deploy modes: node mode, machine mode, standalone mode
- Multi-instance: single process binding multiple panels / nodes

## 本仓库定制版安装

已提供交互式 `install-custom.sh`，无需 VPS 编译。请先阅读 [Debian 13 一键安装及实测记录](docs/DEPLOYMENT_ZH.md)。下方上游安装方式不包含本仓库 AnyTLS + REALITY 修改。2026-09-29 已修复主站 WebSocket 地址/代理和设备列表序列化问题，连接、推送及自动重连已实测；REST 保留，完整同步边界验收仍待完成。

## VPS 网络调优

Mieru 的 transport 修复、原生 TCP 分片对照工具和 XBoard 混淆界面配套补丁见 [Mieru 实验说明](docs/MIERU_LAB_ZH.md)。

[一键 BBR 与高吞吐 TCP 配置](docs/NETWORK_TUNING_ZH.md)：独立于节点安装，可重复执行并回退。

## Install

### Docker

```bash
docker run -d --restart=always --network=host \
  -e apiHost=https://panel.com -e apiKey=TOKEN -e nodeID=1 \
  ghcr.io/cedar2025/xboard-node:latest
```

### Docker Compose

```bash
git clone -b compose --depth 1 https://github.com/cedar2025/xboard-node.git
cd xboard-node
vim config/config.yml   # set panel.url / token / node_id
docker compose up -d
```

### Installer (Linux systemd)

```bash
# Node mode
curl -fsSL https://raw.githubusercontent.com/cedar2025/xboard-node/dev/install.sh | \
  sudo bash -s -- --mode node --panel https://panel.example.com --token TOKEN --node-id 1

# Machine mode
curl -fsSL https://raw.githubusercontent.com/cedar2025/xboard-node/dev/install.sh | \
  sudo bash -s -- --mode machine --panel https://panel.example.com --token TOKEN --machine-id 1
```

## xbctl

Run `xbctl` after installation for help. Common commands:

```bash
xbctl list                          # list all instances
xbctl status                        # running status
xbctl bind add-node --panel URL --token TOKEN --node-id 1
xbctl bind add-machine --panel URL --token TOKEN --machine-id 1
xbctl bind remove-node --panel URL --node-id 1
xbctl service restart
```

## Configuration

Legacy single-panel config is fully compatible. Appending bindings auto-migrates to `instances` format. See `config.yml.example`.

## Extensions

2026-10-01：完成标准 TUIC v5 + TLS 的实机、订阅及 UDP 转发验证，修复首次安装时
未写入证书路径的问题。见 [TUIC 部署验证](docs/TUIC_DEPLOYMENT_ZH.md)。

2026-10-01：修复出站、DNS 和路由依赖的自动更新。已有标准安装可使用
`upgrade-custom.sh`，详见 [热更新实测与旧版升级](docs/HOT_RELOAD_ZH.md)。

2026-09-30：定制 sing-box 面板路由/DNS 修复已通过两个真实节点测试，安装器已更新发行包。
见 [路由验证与回退记录](docs/ROUTING_VERIFICATION_ZH.md)。

- Custom routes: [docs-custom-routes.md](docs-custom-routes.md)
- Custom outbounds: [docs-custom-outbounds.md](docs-custom-outbounds.md)
- DNS providers (ACME DNS-01): [docs-dns-providers.md](docs-dns-providers.md)

## License

MPL-2.0.
