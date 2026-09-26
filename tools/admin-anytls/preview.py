"""Create a local-only browser harness; never deploy this harness to the panel."""
from pathlib import Path
import json
import shutil
import sys

build_dir, upstream_dir, output = map(Path, sys.argv[1:])
metadata = json.loads((build_dir / 'build.json').read_text(encoding='utf-8'))
bundle = (build_dir / metadata['file']).read_text(encoding='utf-8')
mount = 'J.createRoot(document.getElementById("root")).render('
assert bundle.count(mount) == 1
bundle = bundle.replace(mount, 'void(')
bundle += r'''
function CodexPreview() {
  const schema=t5t.anytls.schema;
  const form=Nv({resolver:Mv(schema),defaultValues:schema.parse({tls:1,tls_settings:{server_name:"legacy.example"},padding_scheme:["stop=8"]}),mode:"onChange"});
  const [status,setStatus]=H.useState("Ready: existing standard TLS fixture");
  const labels={
    "dynamic_form.vless.tls.label":"TLS 模式", "dynamic_form.vless.tls.none":"关闭",
    "dynamic_form.vless.tls.tls":"Standard TLS", "dynamic_form.vless.tls.reality":"REALITY",
    "dynamic_form.vless.reality_settings.server_name.label":"Server Name",
    "dynamic_form.vless.reality_settings.private_key.label":"Private Key",
    "dynamic_form.vless.reality_settings.public_key.label":"Public Key",
    "dynamic_form.vless.reality_settings.short_id.label":"Short ID",
    "dynamic_form.anytls.tls.server_name.label":"Server Name",
    "dynamic_form.anytls.padding_scheme.label":"Padding Scheme",
    "dynamic_form.anytls.padding_scheme.use_default":"使用默认方案"
  };
  const translate=(key)=>labels[key]||key;
  const validate=()=>{
    const checked=schema.safeParse(form.getValues());
    if(!checked.success){setStatus("SCHEMA FAILED");return;}
    const d=checked.data,r=d.reality_settings;
    setStatus(`SCHEMA PASS: TLS=${d.tls}; standard SNI=${d.tls_settings.server_name}; reality SNI=${r.server_name}; dest=${r.dest}; key lengths=${r.private_key.length}/${r.public_key.length}; short ID length=${r.short_id.length}; padding rows=${d.padding_scheme.length}`);
  };
  return Q.jsxs("main",{className:"mx-auto max-w-xl space-y-5 p-6",children:[
    Q.jsx("h1",{className:"text-lg font-bold",children:"AnyTLS REALITY — isolated UI test"}),
    Q.jsx(zy,{...form,children:Q.jsx(Zot,{children:Q.jsx(t5t.anytls.component,{form,t:translate})})}),
    Q.jsx("button",{type:"button",onClick:validate,className:"rounded border px-4 py-2",children:"检查表单"}),
    Q.jsx("p",{role:"status",children:status})
  ]});
}
const defaultSettings=t5t.anytls.schema.parse({});
if(defaultSettings.tls!==1||!defaultSettings.tls_settings||!("dest" in defaultSettings.reality_settings))throw Error("Canonical schema/default regression");
J.createRoot(document.getElementById("test-root")).render(Q.jsx(CodexPreview,{}));
'''
output.mkdir(parents=True, exist_ok=True)
(output / 'preview.js').write_text(bundle, encoding='utf-8')
shutil.copyfile(upstream_dir / 'assets/index-CYqGixs-.css', output / 'style.css')
(output / 'index.html').write_text('''<!doctype html><html><head><meta charset="utf-8"><title>AnyTLS isolated UI test</title>
<link rel="stylesheet" href="style.css"><script>window.settings={base_url:"/",title:"Local UI test",version:"test",logo:"",secure_path:"test"};
window.addEventListener('error',e=>{document.querySelector('#error').textContent=e.message});</script></head>
<body><pre id="error"></pre><div id="test-root"></div><script type="module" src="preview.js"></script></body></html>''', encoding='utf-8')
print('Local preview prepared; no production APIs or credentials used.')
