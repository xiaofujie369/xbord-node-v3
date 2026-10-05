# Mieru TCP / XBoard 实验

使用 XBoard 主站和 node v3 管理 Mieru TCP 对照实验；在独立节点和账号上测试，修改前备份配置。

## 当前实现

- Mieru 由 node v3 内嵌 sing-box 启动，不依赖独立 mita 容器。
- 仍使用仓库固定的 Mieru 3.29.0；没有将独立容器的 3.38.0 冒充 node 内嵌版本。
- 修复缺省/小写 transport 导致内核拒绝配置：缺省 TCP，规范为大写。
- XBoard 的 Mihomo 订阅补充 `traffic-pattern`；端口范围与单端口互斥。
- `cmd/mieru-lab` 生成经过原生库验证的模式，并用同版本官方 API 做限时、限量、无本地监听器的 HTTPS 请求验证。

## 对照设计

基线与分片均固定 seed=20261005、保留随机 nonce、unlockAll=false，不修改加密算法。唯一变量为 tcpFragment.enable；分片最大等待为 0 ms，先验证轻量分片开销。这里的混淆是原生流量形态配置，不是新加密协议。

| 组别 | TCP 分片 | 示例端口 |
| --- | --- | --- |
| baseline | 关闭 | 20001 |
| fragment | 开启 | 20002 |

建议两个节点使用独立测试组。测试用户 UUID 遵循现有 XBoard 认证映射；账号、订阅 URL 和私有配置不得提交 Git。

管理端的四种流量混淆选项和订阅导出补丁见 [XBoard 配套集成](../integrations/xboard/mieru/README.md)。仅更新 node 二进制不会自动修改面板界面。

## 使用

```bash
go run ./cmd/mieru-lab
go run ./cmd/mieru-lab -config /private/client-baseline-user-1.json
go run ./cmd/mieru-lab -config /private/client-fragment-user-1.json
go test ./cmd/mieru-lab ./internal/kernel/singbox
```

每次检查 3 个固定 HTTPS 目标各 3 次；整个命令最长 120 秒，单请求最长 20 秒，响应体最多读取约 1 MiB。JSONL 记录状态、TTFB、耗时、字节数和错误；不打印凭据。TTFB 包含代理路径、目标 TLS/响应等待，不能当作单纯握手时间。短时小响应测试不是吞吐、视频或长连接验收。

## 兼容性与后续实验

- Mihomo 的原生字段为 `traffic-pattern`，应使用确认支持 Mieru 的内核；GUI 名称不能代表内核兼容性。
- 官方客户端/服务端可独立设置 traffic pattern；双端不强制相同。订阅补齐是为了让面板选定的实验配置也作用于客户端发送方向。
- 当前官方新版含 padding / lowEntropy；3.29.0 实验不启用这些新字段。升级应单独分支、验证 node 内嵌接口和客户端版本，再测试增加的流量与 CPU 开销。
- 先进行跨运营商、重复交错 A/B、100 MB 下载、1/6/24 小时会话和弱网恢复测试，再判断是否进入正式使用。
- 本次不声称能规避特定网络识别，也不以几次成功请求判断长期封锁风险。

## 回退

在 XBoard 中禁用对应实验节点，node v3 会接收配置更新。禁用前确认节点尚未被正式用户使用；不要卸载整个 node 服务或影响其他绑定节点。恢复面板文件或节点二进制前对照当前差异，避免覆盖后来新增修改。

参考：https://github.com/enfein/mieru/blob/v3.29.0/docs/traffic-pattern.md
