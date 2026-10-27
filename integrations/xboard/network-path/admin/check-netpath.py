# 检查 build-netpath.py 生成的资源：Chromium 语法解析 + 辅助函数逻辑。用法：python check-netpath.py BUNDLE netpath-helpers.js
import sys, json, pathlib
from playwright.sync_api import sync_playwright
bundle = pathlib.Path(sys.argv[1]).read_text(encoding="utf-8")
helpers = pathlib.Path(sys.argv[2]).read_text(encoding="utf-8")
with sync_playwright() as p:
    b = p.chromium.launch(); pg = b.new_page()
    # 1) 语法：作为模块编译（只解析，不执行导入）
    r = pg.evaluate("""src => { try { new Function(src.replace(/^\\s*import[^;]*;/gm, "").replace(/\\bimport\\.meta\\b/g,"({})").replace(/\\bimport\\(/g,"(x=>x)(")); return "syntax ok"; } catch (e) { return "SyntaxError: " + e.message; } }""", bundle)
    print(r)
    # 2) 辅助函数逻辑
    r = pg.evaluate("""h => { eval(h + `;window.__t={kwNetFields,kwGetPath,kwSetPath,kwNetSummary}`); const T = window.__t, out = {};
      let o = {}; T.kwSetPath(o, "headers.Host", "a.com"); T.kwSetPath(o, "path", "/x"); out.ws = JSON.stringify(o);
      T.kwSetPath(o, "headers.Host", ""); out.wsClearHost = JSON.stringify(o);
      out.sumWs = T.kwNetSummary({path:"/abc"}, "ws"); out.sumEmpty = T.kwNetSummary({}, "ws"); out.sumGrpc = T.kwNetSummary({}, "grpc");
      out.sumTcp = T.kwNetSummary({}, "tcp"); out.getHost = T.kwGetPath({headers:{Host:"h"}}, "headers.Host");
      let h2 = {}; T.kwSetPath(h2, "host", ["a","b"]); out.h2 = JSON.stringify(h2);
      let keep = {path:"/p", headers:{Host:"h", "User-Agent":"x"}, max_early_data:2048}; T.kwSetPath(keep, "headers.Host", ""); out.keepOthers = JSON.stringify(keep);
      return out; }""", helpers)
    print(json.dumps(r, ensure_ascii=False, indent=1))
    b.close()
