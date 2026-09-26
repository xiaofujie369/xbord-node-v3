"""Build a fail-closed, version-pinned adapter for an upstream admin bundle.

Usage: python build.py ORIGINAL_BUNDLE OUTPUT_DIRECTORY
The original bundle is never overwritten. No npm dependency or new crypto code.
"""
import hashlib
import json
from pathlib import Path
import sys

EXPECTED_SHA256 = '87ca206dbaa4d5660a5697679ec2c3737dcb45659b9e521e5c14d5f4ac6e4100'


def between(text, start, end):
    if text.count(start) != 1 or text.count(end) != 1:
        raise ValueError(f'Upstream boundary changed: {start}')
    first = text.index(start)
    last = text.index(end, first)
    return first, last, text[first:last]


def build(source, extension):
    if hashlib.sha256(source).hexdigest() != EXPECTED_SHA256:
        raise ValueError('Unexpected upstream bundle SHA-256; review the new version before patching')
    text = source.decode('utf-8')
    schema_start, schema_end, schema = between(text, 'z4t=(e,t)=>', ',U4t=[')
    component_start, component_end, component = between(text, '$4t=({form:e,t:t})=>', ',q4t=e=>')
    old_schema = schema[len('z4t='):]
    old_component = component[len('$4t='):]
    # Only the existing AnyTLS component is adapted to the canonical TLS object.
    # Translation keys stay unchanged; no other protocol component is rewritten.
    old_component = old_component.replace('name:"tls.', 'name:"tls_settings.')
    old_component = old_component.replace('prefix:"tls.ech"', 'prefix:"tls_settings.ech"')
    factory = extension.replace('export function createAnyTLSRealityExtension', 'function createAnyTLSRealityExtension', 1)
    bindings = ('{jsx:Q.jsx,jsxs:Q.jsxs,Fragment:Q.Fragment,legacySchema:' + old_schema
                + ',legacyComponent:' + old_component
                + ',vlessSchema:I4t,vlessComponent:O4t,object:py,number:yy,string:cy,'
                  'FormField:$y,FormItem:Gy,FormLabel:Zy,FormControl:Yy,Input:u8e}')
    replacement = 'codexAnyTLSReality=(' + factory + ')(' + bindings + '),z4t=codexAnyTLSReality.schema'
    text = text[:component_start] + '$4t=codexAnyTLSReality.Component' + text[component_end:]
    text = text[:schema_start] + replacement + text[schema_end:]
    cert = 'certPath:"cert_config"in(t5t[l]?.schema?.shape||{})?"cert_config":void 0'
    if text.count(cert) != 1:
        raise ValueError('Upstream advanced-certificate control changed')
    text = text.replace(cert, 'certPath:"anytls"===l&&Number(x.watch("protocol_settings.tls"))!==1?null:("cert_config"in(t5t[l]?.schema?.shape||{})?"cert_config":void 0)')
    return text.encode('utf-8')


if __name__ == '__main__':
    source_path, output_dir = map(Path, sys.argv[1:])
    result = build(source_path.read_bytes(), Path(__file__).with_name('extension.mjs').read_text(encoding='utf-8'))
    digest = hashlib.sha256(result).hexdigest()
    output_dir.mkdir(parents=True, exist_ok=True)
    filename = f'index-anytls-reality-{digest[:12]}.js'
    (output_dir / filename).write_bytes(result)
    metadata = {'file': filename, 'sha256': digest, 'upstream_sha256': EXPECTED_SHA256,
                'upstream_commit': 'fe6dc2952241c6bed807d1ca965e7d5907303265'}
    (output_dir / 'build.json').write_text(json.dumps(metadata, indent=2), encoding='utf-8')
    print(json.dumps(metadata))
