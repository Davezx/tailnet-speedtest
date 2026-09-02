# tailnet-speedtest

在 tailnet 内网中，用浏览器测量任意设备到服务器的链路质量：**下行/上行带宽（单流 + 4 并发）、空闲与负载下延迟（RTT）、抖动、下行 TCP 重传率**，并显示连接走的是 **direct 直连还是 DERP 中继**。所有结果以 JSONL 追加存服务端文件，任何设备打开都能看到全量历史与趋势。

单个 Go 二进制，前端页面用 `embed` 内嵌，存储为零依赖的 JSON Lines 文件（数据量极小，无需数据库），无 CGO、无运行时依赖。

## 测量原理

| 指标 | 方法 |
|---|---|
| 空闲延迟 / 抖动 | WebSocket 回显 100 个消息（10ms 间隔），RTT 取 avg/min/max，抖动按 RFC 3550 递推 |
| 负载下延迟 | WS 连接在整个测试期间保活，带宽测试阶段每 250ms 继续 ping——暴露中继链路的 bufferbloat |
| 下行/上行带宽 | 单流与 4 并发各测一次（目标时长 8s，先探测估算带宽再定数据量）；统计丢弃前 15% 样本（TCP 慢启动）。单流与多流差距大说明单连接受 TCP 窗口限制 |
| 下行 TCP 重传 | 服务端在下载连接上 `getsockopt(TCP_INFO)` 读取该连接重传段数（Linux）。上行方向的重传发生在客户端 TCP 栈，服务端读不到，故只报下行 |
| direct / relay | 服务端执行 `tailscale status --json` 匹配客户端 tailnet IP |

不提供"丢包率"数字：TCP/WS 之上的丢包统计会被重传掩盖，是误导性指标；链路丢包以重传率 + 负载延迟尖峰呈现。

## 使用

```bash
go build -o tailnet-speedtest .
nohup ./tailnet-speedtest -addr <tailnet-IP>:8081 -db speedtest.jsonl > run.log 2>&1 & echo $! > run.pid
# 浏览器打开 http://<tailnet-IP>:8081；用完 kill $(cat run.pid)
```

## 部署（systemd，可选常驻）

```bash
./install.sh            # 编译 → /usr/local/bin → systemd enable --now，监听 :8080
./install.sh -addr 100.x.y.z:8080   # 只绑 tailnet 接口 IP
```

`install.sh` 会优先使用项目内 `.toolchain/go`（若存在），否则用系统 `go`。service 以 root 运行（需读 `tailscaled` socket 获取 whois 身份与 direct/relay 信息），数据库放在 `/var/lib/tailnet-speedtest/`（`StateDirectory`）。

## 防护

- 全局同时只允许 1 个测试（并行测试互相污染带宽数字）
- 每客户端 IP 每分钟最多 3 次测试，超限返回 429
- `-max-download` 单次下载上限默认 512MB

## 参数

- `-addr`：监听地址，默认 `:8080`
- `-db`：结果存储路径（JSON Lines），默认 `./speedtest.jsonl`
- `-max-download`：单次下载请求字节上限，默认 512MB

## HTTPS（可选）

纯 HTTP 在 tailnet 内已由 WireGuard 加密，一般够用。想要浏览器绿锁可用 `tailscale serve`：

```bash
tailscale serve --bg 8080   # 然后访问 https://<机器名>.<tailnet>.ts.net
```

## 文件

- `main.go` — 入口、路由、ConnContext（暴露连接给 TCP_INFO）
- `server.go` — download/upload/results/limiter/info 端点
- `ws.go` — WebSocket 延迟回显（跨测试阶段保活）
- `db.go` — JSONL 结果存储
- `ratelimit.go` — 全局并发与每 IP 限流
- `tailscale.go` — whois 身份归属、direct/relay 查询
- `static/index.html` — 前端单页（测速 + 历史趋势两个 tab，embed 进二进制）
- `tailnet-speedtest.service`、`install.sh` — systemd 部署
