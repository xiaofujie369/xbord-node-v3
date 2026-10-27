# XBoard 传输协议路径输入框

XBoard 管理端的「编辑协议配置」弹窗原本只有一个 JSON 编辑框，入口是传输协议旁一个不显眼的「编辑协议」链接。本补丁做了两处修改：

- 链接直接显示当前路径，例如「路径：/abc · 编辑协议」；
- 弹窗顶部加入「路径」和「Host」输入框（grpc 为 serviceName），并提供「随机」按钮生成随机路径。输入内容会同步写入下方 JSON，JSON 里的其它键保持不变。

改动只涉及 `u4t`（network_settings 编辑弹窗），VMess、VLESS、Trojan 表单都使用这个组件。
支持的传输方式和写入的字段：

| 传输 | 写入字段 |
| --- | --- |
| ws | `path`、`headers.Host` |
| httpupgrade / xhttp | `path`、`host` |
| h2 | `path`、`host`（数组） |
| grpc | `serviceName` |

这些字段与 XBoard 订阅生成（`app/Protocols/*`）和 xbord-node-v3 内核配置（`internal/kernel/*/config.go`）读取的字段一致。

## 构建与部署

补丁基于已部署的 Mieru 定制版管理端（`index-mieru-obfs-4cc67fefd467.js`）。输入资源的哈希不符或替换点不唯一时，脚本会拒绝构建。

```sh
python admin/build-netpath.py OLD_BUNDLE OLD_MANIFEST OUTPUT_DIR
```

- 输入资源 SHA-256：`4cc67fefd46757279b8760b7537ea1171c9caecc69b81944c5f7fc99a90e6c68`
- 输出资源：`index-netpath-c0b8be1b8791.js`，SHA-256 `c0b8be1b87915a558387016d5cdc7b41ef40f08a77f7fb8fbdb3168ebecfa449`

部署步骤：

1. 把生成的 JS 放进 `public/assets/admin/assets/`，核对哈希；
2. 备份 `public/assets/admin/manifest.json`，再原子替换为生成的 manifest。

要回退，恢复旧的 manifest 即可。旧资源保留在原处。

## 验证（2026-10-27）

- `admin/check-netpath.py`：整个资源能被 Chromium 解析；辅助函数共 9 个场景，包括清空 Host 时保留 `User-Agent` 与 `max_early_data`。
- 真实站点加载新资源：没有 JS 错误。
- 管理端在浏览器中实测，后端接口用模拟数据，节点为真实的 Trojan/WS 节点：
  - 链接显示路径；
  - 输入框写入 JSON，「随机」按钮可用，重新打开能回显；
  - 提交的 `network_settings` 为 `{"path":"/…","headers":{"Host":"…"}}`，其它字段不变。
- 端到端：在面板修改 Trojan/WS 节点路径后，xbord-node-v3（sing-box 内核）在一分钟内生效，旧路径返回 404、新路径返回 101。经 Cloudflare 的 Trojan 隧道出口为落地节点 IP。

升级管理端后要重新审查这个补丁，不要删掉哈希检查强行构建。
