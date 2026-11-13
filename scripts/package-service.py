#!/usr/bin/env python3
"""Build isolated public Linux service bundles, without local state or LAN flags."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tarfile
import tempfile

ROOT = Path(__file__).resolve().parent.parent

QUICKSTART = """# Agent Hub 公网测试包

本包包含一个静态链接的 Go 服务和配套 Flutter Web 客户端，不需要安装 Go、Flutter 或 Node。
不包含开发机数据、设备身份、配对密钥、TLS 证书或聊天记录。首次启动创建 ./data/devices.json，请持久化并备份 data 目录。

## 端口

- 26330/TCP：HTTPS、Web、API、WSS 信令与业务中继。
- 26330/UDP：STUN，辅助两端发现公网地址并尝试直连。

TCP 和 UDP 可以同时使用同一个数字端口。服务器防火墙/云安全组需要分别放行两种协议。
UDP 不能由普通 HTTP 反代转发，也不需要配置一段 UDP 中继端口。

## 直接 HTTPS 启动

将域名解析到服务器，并准备与域名匹配的证书：

```sh
chmod +x agenthub-service start.sh
./start.sh \\
  --tls-cert /absolute/path/fullchain.pem \\
  --tls-key /absolute/path/privkey.pem \\
  --stun-public stun:hub.example.com:26330
```

浏览器访问 https://hub.example.com:26330 。桌面应用的「服务设置」填写相同的 HTTPS 地址。
Web 自动使用页面 origin，手机/桌面也可手动配置服务地址，不需要为更换服务器重编译客户端。
初次切换到新服务需要在该服务重新登记/配对。被控电脑须运行已接入 Agent 的 Agent Hub。

前台日志可用于调试，Ctrl+C 停止服务。需要后台常驻时，可交给已有的 systemd 或进程管理器。

## 已有 HTTPS 反代

```sh
./start.sh --listen 127.0.0.1:26330 --stun-public stun:udp.example.com:26330
```

将已有 HTTPS 域名的 HTTP/WebSocket 请求反代到 127.0.0.1:26330；UDP 26330 直接开放到服务器。
包内 nginx.conf.example 提供 Nginx 的 HTTPS/WSS 配置示例，修改证书路径后执行 nginx -t 再加载。
这种部署下访问 https://hub.example.com，客户端也填写这个地址，不加后端的 26330 端口。
如果 Web 域名经过不支持 UDP 的 CDN，stun-public 应填写直连服务器的域名或公网 IP。
只有前后端分开托管时才需要 --origins https://web.example.com；同源访问不需要额外配置。

公网客户端要求 HTTPS/WSS。本包 Web 是正式网络策略构建，未启用开发用的私网 HTTP 例外。

## 检查

```sh
curl --fail https://hub.example.com:26330/healthz
curl --fail -H 'Content-Type: application/json' -d '{}' https://hub.example.com:26330/v2/network
```

healthz 返回 ok；network 中 selfStun 应指向可直达的公网 UDP 地址。
公共 STUN 与自建 STUN 同时辅助直连，连接不可直连时统一走本服务 WSS 中继。
MVP 不启用 TURN、DERP 或 Tailcat。./agenthub-service --help 可查看全部参数。

## 修改端口

```sh
./start.sh --listen :26335 --stun-listen :26335 \\
  --stun-public stun:hub.example.com:26335 \\
  --tls-cert /absolute/path/fullchain.pem --tls-key /absolute/path/privkey.pem
```

更改端口时同步配置 TCP、UDP 和对外发布的 STUN 地址。
此版本用于公网联调，设备配额/计费与多节点共享状态尚未实现，不支持多个实例同时写同一个 data 目录。
"""

START = """#!/bin/sh
set -eu
cd "$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
exec ./agenthub-service --state ./data/devices.json --web ./web "$@"
"""


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--version', default='0.1.0-mvp')
    parser.add_argument('--domain', default='', help='public hostname used by bundled startup script and instructions')
    args = parser.parse_args()
    if not args.version or any(c not in 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789.-_' for c in args.version):
        parser.error('version must contain only letters, digits, dot, dash or underscore')
    if args.domain and not re.fullmatch(r'[a-zA-Z0-9](?:[a-zA-Z0-9.-]*[a-zA-Z0-9])?', args.domain):
        parser.error('domain must be an ASCII hostname without a scheme, port or path')
    release = ROOT / 'build/releases'
    release.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='.service-package-', dir=release) as temporary:
        work = Path(temporary)
        web = work / 'web'
        env = dict(os.environ)
        env['AGENTHUB_ALLOW_LAN_HTTP'] = 'false'
        env['AGENTHUB_WEB_OUTPUT'] = str(web)
        env.pop('AGENTHUB_SERVICE', None)
        subprocess.run(['bash', 'scripts/build-mvp.sh', 'web'], cwd=ROOT, env=env, check=True)
        if not (web / 'index.html').is_file() or not (web / 'main.dart.js').is_file():
            raise SystemExit('Web release output is incomplete')
        checksums = []
        for arch in ('amd64', 'arm64'):
            name = f'agenthub-service-{args.version}-linux-{arch}'
            bundle = work / name
            bundle.mkdir()
            goenv = dict(env, GOOS='linux', GOARCH=arch, CGO_ENABLED='0')
            goenv['GOAMD64'] = 'v1'
            subprocess.run(['go', 'build', '-trimpath', '-ldflags=-s -w', '-o', str(bundle / 'agenthub-service'), './cmd/agenthub-service'], cwd=ROOT, env=goenv, check=True)
            shutil.copytree(web, bundle / 'web')
            shutil.copy2(ROOT / 'deploy/service/README.md', bundle / 'DEPLOYMENT.md')
            shutil.copy2(ROOT / 'deploy/service/nginx.conf.example', bundle / 'nginx.conf.example')
            quickstart, start = QUICKSTART, START
            if args.domain:
                quickstart = quickstart.replace('hub.example.com', args.domain).replace('udp.example.com', args.domain)
                start = start.replace('--web ./web', '--web ./web --stun-public "${AGENTHUB_STUN_URL:-stun:' + args.domain + ':26330}"')
            (bundle / 'README.md').write_text(quickstart)
            (bundle / 'start.sh').write_text(start)
            (bundle / 'start.sh').chmod(0o755)
            (bundle / 'agenthub-service').chmod(0o755)
            (bundle / 'release.json').write_text(json.dumps({
                'version': args.version, 'os': 'linux', 'arch': arch,
                'tcpPort': 26330, 'udpPort': 26330,
                'allowLANHTTP': False,
                'domain': args.domain,
            }, indent=2) + '\n')
            files = sorted(p for p in bundle.rglob('*') if p.is_file())
            (bundle / 'SHA256SUMS').write_text(''.join(
                f'{hashlib.sha256(p.read_bytes()).hexdigest()}  {p.relative_to(bundle)}\n' for p in files))
            archive = release / f'{name}.tar.gz'
            with tarfile.open(archive, 'w:gz') as tar:
                tar.add(bundle, arcname=name)
            digest = hashlib.sha256(archive.read_bytes()).hexdigest()
            checksums.append(f'{digest}  {archive.name}\n')
            print(f'Built {archive} ({archive.stat().st_size / (1 << 20):.1f} MiB)', flush=True)
        (release / f'agenthub-service-{args.version}-SHA256SUMS').write_text(''.join(checksums))


if __name__ == '__main__':
    main()
