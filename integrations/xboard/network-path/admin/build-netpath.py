"""在已部署的定制管理端（Mieru 版）上，为传输协议配置弹窗增加「路径 / Host / serviceName」输入框。

只改 u4t（network_settings 编辑弹窗）这一个组件，VMess / VLESS / Trojan 表单都使用它。
输入资源哈希不符或替换点不唯一时拒绝构建。
"""
import argparse
import hashlib
import json
from pathlib import Path

here = Path(__file__).parent
parser = argparse.ArgumentParser()
parser.add_argument("bundle", type=Path)
parser.add_argument("manifest", type=Path)
parser.add_argument("output", type=Path)
args = parser.parse_args()
raw = args.bundle.read_bytes()
expected = "4cc67fefd46757279b8760b7537ea1171c9caecc69b81944c5f7fc99a90e6c68"
if hashlib.sha256(raw).hexdigest() != expected:
    raise SystemExit("Unexpected input bundle. Review the new version before patching.")
source = raw.decode("utf-8")

anchor = "function u4t({dialogTrigger:e,value:t,setValue:n,templateType:i}){"
trigger = 'e??Q.jsx(xtt,{variant:"link",children:r("network_settings.edit_protocol")})'
dialog = 'Q.jsxs("div",{className:"space-y-4",children:[g().length>0&&Q.jsx("div",{className:"flex flex-wrap gap-2 pt-2"'
start = source.index(anchor) if source.count(anchor) == 1 else None
if start is None:
    raise SystemExit("u4t boundary is not unique")
end = source.index("const h4t=", start)
body = source[start:end]
for marker in (trigger, dialog):
    if body.count(marker) != 1:
        raise SystemExit(f"marker not unique inside u4t: {marker[:50]}")

body = body.replace(trigger, 'e??Q.jsx(xtt,{variant:"link",children:kwNetFields[i]?("grpc"===i?"serviceName：":"路径：")+kwNetSummary(t,i)+" · "+r("network_settings.edit_protocol"):r("network_settings.edit_protocol")})', 1)
body = body.replace(dialog, 'Q.jsxs("div",{className:"space-y-4",children:[Q.jsx(kw4t,{type:i,text:a,setText:l,setError:d,validate:u}),g().length>0&&Q.jsx("div",{className:"flex flex-wrap gap-2 pt-2"', 1)
helpers = (here / "netpath-helpers.js").read_text(encoding="utf-8").strip() + (here / "netpath-form.js").read_text(encoding="utf-8").strip()
patched = (source[:start] + helpers + body + source[end:]).encode("utf-8")

digest = hashlib.sha256(patched).hexdigest()
name = f"index-netpath-{digest[:12]}.js"
args.output.mkdir(parents=True, exist_ok=True)
(args.output / name).write_bytes(patched)
manifest = json.loads(args.manifest.read_text(encoding="utf-8-sig"))
manifest["index.html"]["file"] = "assets/" + name
(args.output / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
(args.output / "build.json").write_text(json.dumps({"file": name, "sha256": digest, "base_sha256": expected}, indent=2) + "\n", encoding="utf-8")
print(json.dumps({"file": name, "sha256": digest}))
