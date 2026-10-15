"""Run the real renewal function/hook with isolated files and mocked systemd/ACME."""
import os
import pathlib
import subprocess
import tempfile

source = (pathlib.Path(__file__).resolve().parents[1] / 'install-custom.sh').read_text()
function = source.split('certbot_run() {', 1)[1].split('\nif [[ ${1:-}', 1)[0]
function = 'certbot_run() {' + function
for case in ['valid', 'lock-retry', 'renew-failure', 'missing-key', 'no-cert']:
    with tempfile.TemporaryDirectory() as tmp:
        root = pathlib.Path(tmp)
        etc = root / 'etc'
        cert = etc / 'letsencrypt/live/test.example.com'
        node = etc / 'xboard-node-custom'
        fake = root / 'bin'
        for directory in [cert, node, fake, etc / 'letsencrypt/renewal']:
            directory.mkdir(parents=True, exist_ok=True)
        subprocess.run(['openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes',
                        '-subj', '/CN=test.example.com', '-days', '1',
                        '-keyout', str(cert / 'privkey.pem'), '-out', str(cert / 'fullchain.pem')],
                       check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        (etc / 'letsencrypt/renewal/test.example.com.conf').touch()
        # Also exercises existing YAML config, not just JSON written by installer.
        (node / 'config.yml').write_text('{}' if case == 'no-cert' else
            f'cert:\n  cert_mode: file\n  cert_file: {cert}/fullchain.pem\n  key_file: {cert}/privkey.pem\n')
        if case == 'missing-key':
            (cert / 'privkey.pem').unlink()
        def command(name, body):
            p = fake / name
            p.write_text('#!/bin/sh\nset -eu\n' + body + '\n')
            p.chmod(0o755)
        command('systemctl', 'echo "$*" >> "$TEST_LOG"')
        command('sleep', 'exit 0')
        lock = ('if [ ! -f "$TEST_ROOT/locked-once" ]; then touch "$TEST_ROOT/locked-once"; echo "Another instance of Certbot is already running."; exit 1; fi\n' if case == 'lock-retry' else '')
        command('certbot', lock + 'echo "$*" >> "$TEST_LOG"\n' +
                ('exit 1' if case == 'renew-failure' else
                 f'RENEWED_LINEAGE="{cert}" "{etc}/letsencrypt/renewal-hooks/deploy/xboard-node-custom.sh"'))
        runner = root / 'run.sh'
        runner.write_text('set -Eeuo pipefail\nfail() { echo "$*" >&2; exit 1; }\n' +
                          function.replace('/etc/', str(etc) + '/') +
                          '\nUNIT=xboard-node-custom.service\nTMP_DIR="$TEST_ROOT"\nconfigure_renewal\n')
        env = dict(os.environ, PATH=str(fake) + ':' + os.environ['PATH'],
                   TEST_ROOT=str(root), TEST_LOG=str(root / 'calls'))
        result = subprocess.run(['bash', str(runner)], env=env, capture_output=True, text=True)
        assert (result.returncode == 0) == (case in ['valid', 'lock-retry', 'no-cert']), (case, result.stdout, result.stderr)
        if case == 'valid':
            log = (root / 'calls').read_text()
            assert '--dry-run --run-deploy-hooks' in log
            assert 'try-restart xboard-node-custom.service' in log
            hook = etc / 'letsencrypt/renewal-hooks/deploy/xboard-node-custom.sh'
            subprocess.run([str(hook)], env=dict(env, RENEWED_LINEAGE='/unrelated'), check=True)
            assert (root / 'calls').read_text() == log, 'unrelated certificate restarted node'
            # A mismatched key must not restart the service.
            subprocess.run(['openssl', 'genrsa', '-out', str(cert / 'privkey.pem'), '2048'],
                           check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            bad = subprocess.run([str(hook)], env=dict(env, RENEWED_LINEAGE=str(cert)), capture_output=True)
            assert bad.returncode != 0 and (root / 'calls').read_text() == log
        print(case, 'PASS')
