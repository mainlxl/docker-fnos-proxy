# Docker Registry Proxy

基于 [xiaoshouchen/docker-fnos-proxy](https://github.com/xiaoshouchen/docker-fnos-proxy) 改造，适配飞牛 NAS 的 Docker 镜像加速服务（docker.fnnas.com）。

## 功能

- 透明代理 Docker Registry V2 API，客户端无需额外配置认证
- 内部自动获取 Harbor Bearer Token（带缓存）
- 官方镜像自动补全 `library/` 前缀
- Blob 下载 CDN 重定向自动跟随
- 从 Docker config.json 动态读取上游认证头（X-Meta-Sign / X-Meta-Token）
- 支持 TLS（HTTPS）
- 支持后台守护进程模式
- 日志自动轮转（lumberjack）

## 编译

需要 Go 1.21+。

```bash
# 本机编译
go build -o docker-fnos-proxy .

# 交叉编译 Linux x86-64
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o docker-fnos-proxy-linux-amd64 .
```

## 使用

```bash
# 前台运行
./docker-fnos-proxy -addr :5001

# 后台运行
./docker-fnos-proxy -d -addr :5001

# 指定配置文件
./docker-fnos-proxy -addr :5001 -docker-config /path/to/config.json

# 停止
kill $(cat docker-fnos-proxy.pid)
```

### 命令行参数

| 参数 | 默认值 | 说明 |
|---|---|---|
| `-addr` | `:5001` | 监听地址 |
| `-tls-cert` | 空 | TLS 证书文件路径（留空则 HTTP） |
| `-tls-key` | 空 | TLS 私钥文件路径 |
| `-d` | `false` | 后台守护进程模式 |
| `-log` | `docker-fnos-proxy.log` | 日志文件路径（守护进程模式生效） |
| `-docker-config` | 空 | Docker config.json 路径 |

### 配置文件查找顺序

不传 `-docker-config` 时，启动时按以下顺序查找：

1. `$HOME/.docker/config.json`
2. `/app/config.json`

找到第一个存在的文件即使用。配置文件格式：

```json
{
  "HttpHeaders": {
    "X-Meta-Sign": "xxx",
    "X-Meta-Token": "xxx"
  }
}
```

这些头由飞牛 NAS 系统自动生成并写入 `~/.docker/config.json`，代理每次请求时实时读取，确保始终使用最新的认证信息。

## Docker 客户端配置

### 方式一：配置 registry-mirrors（推荐）

编辑 `/etc/docker/daemon.json`：

```json
{
  "registry-mirrors": ["http://代理地址:5001"],
  "insecure-registries": ["代理地址:5001"]
}
```

```bash
sudo systemctl restart docker
docker pull nginx
```

### 方式二：直接指定代理地址

```bash
docker pull 代理地址:5001/library/nginx:latest
```

## 架构

```
Docker Client
    │
    │  docker pull nginx
    ▼
Docker Proxy (:5001)
    │
    ├─→ docker.fnnas.com/service/token  (获取 Harbor token)
    │
    ├─→ docker.fnnas.com/v2/...  (拉取 manifest)
    │
    └─→ CDN  (跟随重定向，下载 blob)
    │
    ▼
Docker Client 收到镜像数据
```

## 致谢

- [xiaoshouchen/docker-proxy](https://github.com/xiaoshouchen/docker-proxy) — 原始项目
