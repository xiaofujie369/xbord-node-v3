"""Build a narrowly scoped patch against the verified deployed admin bundle."""
import argparse
import hashlib
import json
from pathlib import Path

parser = argparse.ArgumentParser()
parser.add_argument("bundle", type=Path)
parser.add_argument("manifest", type=Path)
parser.add_argument("output", type=Path)
args = parser.parse_args()
raw = args.bundle.read_bytes()
expected = "a04f8dd4c68007d123ca3b15fb38b9d3816a7ff907e2c862422bde6b92fe02d9"
if hashlib.sha256(raw).hexdigest() != expected:
    raise SystemExit("Unexpected input bundle. Review the new version before patching.")
source = raw.decode("utf-8")
start_marker = "e5t=({form:e,t:t})=>"
end_marker = ",t5t=((e,t)=>"
if source.count(start_marker) != 1 or source.count(end_marker) != 1:
    raise SystemExit("Mieru component boundaries are not unique")
start = source.index(start_marker)
end = source.index(end_marker, start)
replacement = Path(__file__).with_name("mieru-form.js").read_text(encoding="utf-8")
patched = (source[:start] + "e5t=" + replacement + source[end:]).encode("utf-8")
digest = hashlib.sha256(patched).hexdigest()
name = f"index-mieru-obfs-{digest[:12]}.js"
args.output.mkdir(parents=True, exist_ok=True)
(args.output / name).write_bytes(patched)
manifest = json.loads(args.manifest.read_text(encoding="utf-8-sig"))
manifest["index.html"]["file"] = "assets/" + name
(args.output / "manifest.json").write_text(json.dumps(manifest, indent=2)+"\n", encoding="utf-8")
(args.output / "build.json").write_text(json.dumps({"file": name, "sha256": digest, "base_sha256": expected}, indent=2)+"\n", encoding="utf-8")
print(json.dumps({"file": name, "sha256": digest}))
