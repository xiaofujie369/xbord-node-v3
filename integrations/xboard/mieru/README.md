# XBoard Mieru 配套集成

本目录保存 Mieru 实验的面板订阅补丁和管理端表单补丁，需要分别部署到 XBoard；node 安装脚本不会自动应用它们。

## 订阅导出

`subscription.patch` 将 `protocol_settings.traffic_pattern` 导出为 Mihomo 的 `traffic-pattern`，并在使用 `port-range` 时移除单端口 `port`。在 XBoard 仓库先备份并检查 `git apply --check /path/to/subscription.patch`，通过后再应用。将 `MieruSubscriptionTest.php` 放进 XBoard 的 `tests/Unit/Protocols/`，用该项目 PHPUnit 配置运行。

客户端和服务端可独立配置原生 traffic pattern；该补丁让面板选定的配置也进入客户端发送方向。修改后需要更新客户端订阅。

## 管理端表单

入口：节点管理 → 编辑 Mieru 节点 → 流量混淆。选项包括默认模式、实验基线（关闭分片）、轻量 TCP 分片（实验）和自定义流量特征。保留已有自定义配置，UDP 不提供 TCP 分片预设。

当前补丁针对已编译的定制管理端资源，不能用于任意 XBoard 版本。`admin/build-mieru.py` 检查输入 SHA-256 及唯一组件边界，只替换 Mieru 表单，保留其他定制。升级管理端后必须重新审查；校验失败时不要删除检查强行构建。

```sh
python admin/build-mieru.py OLD_BUNDLE OLD_MANIFEST OUTPUT_DIR
node admin/test-mieru-form.cjs
node --check OUTPUT_DIR/index-mieru-obfs-4cc67fefd467.js
```

输入资源 SHA-256：`a04f8dd4c68007d123ca3b15fb38b9d3816a7ff907e2c862422bde6b92fe02d9`。
输出资源 SHA-256：`4cc67fefd46757279b8760b7537ea1171c9caecc69b81944c5f7fc99a90e6c68`。

备份站点 `public/assets/admin/manifest.json`，先将生成的 JS 完整放进 `public/assets/admin/assets/`，核验哈希，再原子替换 manifest。保留旧资源；恢复旧 manifest 即可回退界面，但不会回退节点配置。

2026-10-06 验证：表单映射、自定义配置保留、回显、TCP 限制和资源语法检查通过；真实面板提交成功，重新打开正确回显分片模式，后台协议配置、权限组和路由未发生额外变化。分片效果仍需线路实测，不代表新增加密算法。
