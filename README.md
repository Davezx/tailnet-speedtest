# tailnet-speedtest

在 tailnet 内网中，用浏览器测量本机到服务器的链路质量：**下行/上行带宽、延迟（RTT）、抖动、双向丢包率估计**，并显示该连接走的是 **direct 直连还是 DERP 中继**（通过本机 `tailscale status` 获取，取不到则不显示）。

单个 Go 二进制，前端页面用 `embed` 内嵌，无任何运行时依赖。

## 测量原理

| 指标 | 方法 |
|---|---|
| 延迟 / 抖动 | WebSocket 回显 100 个带序号的消息（10ms 间隔），客户端计算 RTT，抖动按 RFC 3550 递推 |
| 丢包 | 上行：服务端比对收到的序号；下行：客户端统计未回显的序号。基于 TCP 上的 WS，测的是应用层消息丢失，长时间为 0 属正常 |
| 下行带宽 | `GET /api/download?size=N` 流式下发随机数据；先用 8MB 探测约 1s 估算带宽，再按目标 8s 选定正式数据量，XHR progress 采样画实时曲线，统计时丢弃前 15%（TCP 慢启动） |
| 上行带宽 | 4MB Blob 分块 POST 到 `/api/upload`，服务端读尽即弃；同样先探测再定块数 |

## 使用

```bash
# 直接跑
go build -o tailnet-speedtest .
./tailnet-speedtest -addr :8080
# 浏览器打开 http://<服务器 tailnet IP>:8080
```

## 部署（systemd）

```bash
./install.sh            # 编译 → /usr/local/bin → systemd enable --now，监听 :8080
./install.sh -addr 100.x.y.z:8080   # 只绑 tailnet 接口 IP
```

`install.sh` 会优先使用项目内 `.toolchain/go`（若存在），否则用系统 `go`。service 默认以 root 运行，以便读 `tailscaled` socket 获取 direct/relay 信息；不需要该信息可自行把 unit 改成 `DynamicUser=yes`。

## 参数

- `-addr`：监听地址，默认 `:8080`
- `-max-download`：单次下载请求的字节上限，默认 512MB

## HTTPS（可选）

纯 HTTP 在 tailnet 内已加密（WireGuard），一般够用。想要浏览器绿锁可用 `tailscale serve`：

```bash
tailscale serve --bg 8080   # 然后访问 https://<机器名>.<tailnet>.ts.net
```

## 文件

- `main.go` — 入口、路由
- `server.go` — download/upload/info 端点
- `ws.go` — WebSocket 延迟/抖动/丢包测量
- `static/index.html` — 前端单页（内联 JS/CSS，embed 进二进制）
- `tailnet-speedtest.service`、`install.sh` — systemd 部署
