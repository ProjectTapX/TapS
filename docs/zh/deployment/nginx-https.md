# Nginx 反代 + HTTPS

把 Panel 放到 nginx 后面，用 Let's Encrypt 证书提供 HTTPS。**Daemon 端不用改**（Daemon 自己已经是 wss + 自签证书 + 指纹 pin，由 Panel 主动发起 TLS 连接）。

## 架构图

```
浏览器 ──https──> nginx(443) ──http──> panel(127.0.0.1:24444)
                                          │
                                          └──wss──> daemon(*:24445)  [自签 + 指纹 pin]
```

## 1. 把 Panel 改成只听 loopback

防止有人绕过 nginx 直连 24444。两种做法：

- **改 systemd 单元**：`Environment=TAPS_ADDR=127.0.0.1:24444` → `systemctl restart taps-panel`
- **或在系统设置里**：「**Panel 监听端口**」目前只能配端口（不带 host），如果要锁定 loopback 用前者

## 2. 安装 nginx + certbot

```bash
apt-get install -y nginx certbot python3-certbot-nginx
```

## 3. nginx 站点配置

```bash
cat >/etc/nginx/sites-available/taps <<'EOF'
upstream taps_panel {
    server 127.0.0.1:24444;
    keepalive 16;
}

# WebSocket 升级判断：仅在客户端发了 Upgrade 头时才设
# Connection: upgrade，普通 HTTP 请求走 Connection: close。
# 直接写死 "upgrade" 对所有请求会导致某些 CDN/中间代理误判。
map $http_upgrade $connection_upgrade {
    default upgrade;
    ''      close;
}

server {
    listen 80;
    listen [::]:80;
    server_name taps.example.com;
    # certbot 会改这块；先放行 ACME，其它 301 到 HTTPS
    location /.well-known/acme-challenge/ { root /var/www/html; }
    location / { return 301 https://$host$request_uri; }
}

server {
    listen 443 ssl http2;
    listen [::]:443 ssl http2;
    server_name taps.example.com;

    # certbot 会自动填这两行
    # ssl_certificate     /etc/letsencrypt/live/taps.example.com/fullchain.pem;
    # ssl_certificate_key /etc/letsencrypt/live/taps.example.com/privkey.pem;
    ssl_protocols       TLSv1.2 TLSv1.3;
    ssl_ciphers         HIGH:!aNULL:!MD5;

    # TLS 性能优化：session 缓存 + OCSP stapling
    ssl_session_cache   shared:SSL:10m;
    ssl_session_timeout 1d;
    ssl_stapling        on;
    ssl_stapling_verify on;

    # 文件上传分片单片可达 1 GiB（Daemon 端限制）+ 元数据余量
    client_max_body_size 1100M;

    # 超时设置
    proxy_connect_timeout 10s;       # 本机连 panel 足够；远程反代可适当放大
    proxy_read_timeout    3600s;     # WebSocket 长连接 + SSE 流式进度
    proxy_send_timeout    3600s;     # 大文件上传

    # gzip 压缩：Panel 返回的 JSON / JS / CSS 可在 nginx 层压缩减少传输
    gzip              on;
    gzip_vary         on;
    gzip_min_length   256;
    gzip_proxied      any;
    gzip_types        text/plain application/json application/javascript text/css
                      application/xml text/xml image/svg+xml;

    # 安全响应头
    # Panel 已内置 CSP / X-Frame-Options / nosniff / Referrer-Policy，
    # 以下由 nginx 额外加（Panel 检测到 X-Forwarded-Proto: https 后
    # 也会自动加 HSTS，两份并存合法但冗余——如果想避免冗余，
    # 可删下面的 add_header 改为仅依赖 Panel 内置）。
    add_header Strict-Transport-Security "max-age=31536000; includeSubDomains" always;

    location / {
        proxy_pass http://taps_panel;
        proxy_http_version 1.1;

        # 必须：让 panel 能从 X-Forwarded-For 还原真实客户端 IP
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # 必须：终端 WebSocket 升级（用 map 变量，非 WS 请求走 close）
        proxy_set_header Upgrade    $http_upgrade;
        proxy_set_header Connection $connection_upgrade;

        # 关闭 buffering，让流式下载 / SSE 实时推送 / 大文件上传直通
        proxy_buffering         off;
        proxy_request_buffering off;
    }
}
EOF

ln -s /etc/nginx/sites-available/taps /etc/nginx/sites-enabled/
nginx -t && systemctl reload nginx
```

## 4. 申请证书

```bash
certbot --nginx -d taps.example.com --redirect --agree-tos -m you@example.com
# certbot 会自动注入 ssl_certificate 行 + 80→443 重定向
systemctl reload nginx
```

## 5. 在 Panel 中告诉 gin "信任 nginx"

如果不做这步，**所有客户端 IP 在 Panel 看来都是 127.0.0.1**，限频 / 审计日志 / API Key IP 白名单全失效。

进入「**系统设置**」→「**反向代理信任列表**」：

- 默认值 `127.0.0.1, ::1` 已经覆盖本机 nginx 场景，**不用改**
- 如果 nginx 在另一台主机：把 nginx 服务器 IP 加进去（如 `127.0.0.1, ::1, 10.0.0.5`）
- 支持 CIDR：`127.0.0.1, ::1, 10.0.0.0/24`

保存 → **重启 Panel**：`systemctl restart taps-panel`

> ⚠️ **不重启不会生效**——`gin.Engine.SetTrustedProxies()` 在启动时一次性应用。

## 6. 验证

打开 `https://taps.example.com/`：
- ✅ 浏览器锁标
- ✅ 登录正常
- ✅ 「**审计日志**」/「**登录日志**」里 IP 列**不是** `127.0.0.1` 而是你的真实公网 IP

## 7. 防火墙收尾

```bash
# 仅允许 80 / 443 入站；24444 只对 loopback 开放（已通过 panel 绑 host 实现）
ufw allow 22,80,443/tcp
ufw deny  24444/tcp
ufw deny  24445/tcp   # 如果 daemon 在同机，Panel 走 127.0.0.1 即可；外网不需要
ufw enable
```

如果 Daemon 在**不同机器**，Daemon 主机要保留 24445 入站给 Panel 主机。

---

## 常见问题

### 上传大文件 413 Request Entity Too Large
nginx 默认 body limit 1 MiB。我们已设 `client_max_body_size 1100M;` 覆盖单分片 1 GiB + 元数据余量。如还报 413，检查 nginx 全局 `nginx.conf` 里的 `http {}` 块是否有更小的 `client_max_body_size` 压制了 server 级设置。

### 终端 WebSocket 连不上
- nginx 站点必须有 `map $http_upgrade` + `proxy_set_header Upgrade / Connection` 三件套
- `proxy_read_timeout` 必须够大（默认 60s 不够，已设 3600s）
- 如果 Panel 公开地址未配置，Panel 会拒绝 WS 升级（返回 503 `settings.public_url_required`）

### 改完反代信任列表 Panel 仍记 127.0.0.1
- 确认是否重启了 panel：`systemctl restart taps-panel`
- 确认 nginx 真的传了 `X-Forwarded-For`：`curl -sD - https://taps.example.com/api/healthz | grep -i x-`

### 想让 Daemon 也走 nginx？
**不必要**。Daemon 已经是 HTTPS（自签 + 指纹 pin），中间多一层 nginx 反而要处理 wss 转发 + 重做指纹链路。直接让 Panel 主机直连 Daemon 端口最简单。
