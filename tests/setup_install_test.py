"""Full fresh install and repeat maintenance with isolated paths/mock commands."""
import gzip
import hashlib
import json
import os
import pathlib
import re
import subprocess
import tempfile

source = (pathlib.Path(__file__).resolve().parents[1] / 'install-custom.sh').read_text()
updater = (pathlib.Path(__file__).resolve().parents[1] / 'upgrade-custom.sh').read_text()
for field in ['RELEASE', 'ARCHIVE_SHA', 'BINARY_SHA']:
    assert re.search(r'^'+field+r'=(.+)$', source, re.M)[1] == re.search(r'^'+field+r'=(.+)$', updater, re.M)[1], field
with tempfile.TemporaryDirectory() as tmp:
    root = pathlib.Path(tmp)
    fake = root / 'bin'
    for path in ['bin', 'root', 'run/lock', 'run/systemd/system', 'etc/systemd/system',
                 'etc/letsencrypt/live/test.example.com', 'etc/letsencrypt/renewal']:
        (root / path).mkdir(parents=True)
    (root / 'tty').write_text('https://panel.example.com\n12\nsecret-fixture\ntest.example.com\nadmin@example.com\ny\n')
    (root / 'meminfo').write_text('MemTotal:        524288 kB\n')
    archive = root / 'release.gz'
    archive.write_bytes(gzip.compress(b'fixture-binary', mtime=0))
    cert = root / 'etc/letsencrypt/live/test.example.com'
    subprocess.run(['openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes',
                    '-subj', '/CN=test.example.com', '-days', '1', '-keyout', str(cert / 'privkey.pem'),
                    '-out', str(cert / 'fullchain.pem')], check=True, capture_output=True)
    (root / 'etc/letsencrypt/renewal/test.example.com.conf').touch()
    def command(name, body):
        p = fake / name
        p.write_text('#!/bin/sh\nset -eu\n' + body + '\n')
        p.chmod(0o755)
    for name in ['apt-get', 'ss', 'sleep']:
        command(name, 'exit 0')
    command('systemctl', 'echo "systemctl $*" >> "$TEST_LOG"')
    command('curl', 'echo download >> "$TEST_LOG"\nwhile [ "$#" -gt 0 ]; do if [ "$1" = -o ]; then shift; cp "$TEST_ARCHIVE" "$1"; exit; fi; shift; done; exit 1')
    command('certbot', f'if [ "$1" = renew ]; then RENEWED_LINEAGE="{cert}" "{root}/etc/letsencrypt/renewal-hooks/deploy/xboard-node-custom.sh"; fi')
    for prefix in ['/etc/', '/opt/', '/var/', '/run/', '/root/']:
        source = source.replace(prefix, str(root) + prefix)
    source = source.replace('/dev/tty', str(root / 'tty')).replace('/proc/meminfo', str(root / 'meminfo'))
    source = source.replace('>&3', '>&2')  # prompts must not overwrite fixture answers
    source = source.replace('0cffb212f033b54eca40aee9b5f0f15cee728af83636cf90d624b81ec282e45d', hashlib.sha256(archive.read_bytes()).hexdigest())
    source = source.replace('58ce7b14e5e2d23b6cd2765c2ac1771b1f247e4e7e2df1ea8e61a0d1bcbcdcda', hashlib.sha256(b'fixture-binary').hexdigest())
    runner = root / 'install.sh'
    runner.write_text(source)
    env = dict(os.environ, PATH=str(fake) + ':' + os.environ['PATH'],
               TEST_LOG=str(root / 'calls'), TEST_ARCHIVE=str(archive))
    for stage in ['fresh', 'repeat']:
        result = subprocess.run(['bash', str(runner)], env=env, text=True, capture_output=True)
        assert result.returncode == 0, (stage, result.stdout, result.stderr)
        cfg = json.loads((root / 'etc/xboard-node-custom/config.yml').read_text())
        assert cfg['machine']['token'] == 'secret-fixture'
        assert cfg['cert']['cert_file'] == str(cert / 'fullchain.pem')
        assert 'GOMEMLIMIT=170MiB' in (root / 'etc/systemd/system/xboard-node-custom.service').read_text()
        assert (root / 'calls').read_text().splitlines().count('download') == 1
        assert 'secret-fixture' not in result.stdout + result.stderr
        print(stage, 'PASS')
    assert list((root / 'root').glob('xboard-node-setup-*/config/config.yml'))
