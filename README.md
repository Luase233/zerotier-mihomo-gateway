# ZeroTier → Windows → Mihomo gateway

当前版本：[v0.1.0](https://github.com/Luase233/zerotier-mihomo-gateway/releases/tag/v0.1.0)（实验性预发布）。版本变更见 [CHANGELOG.md](CHANGELOG.md)。

Windows 原生的实验性 IPv4 透明网关：将指定 ZeroTier 客户端的转发数据包转换为本机 Mihomo SOCKS5 连接。不开启 Clash TUN，不创建 Wintun/TUN 网卡，不修改 Windows 默认路由。

```text
iPhone / IPv4 client
  → ZeroTier → WinDivert NETWORK_FORWARD
  → in-process gVisor TCP/IP stack
  → SOCKS5 127.0.0.1:7897 → existing Mihomo → Internet
```

这是经过单台 Windows / iPhone 环境验证的自研原型，不是 WinDivert、ZeroTier、Mihomo 或 tun2socks 官方提供的网关产品，也未经过独立安全审计。源码发布不代表对其他环境的稳定性或防泄漏保证。

## 功能与范围

- 只捕获配置的 IPv4 源地址，filter 为 `ip and ip.SrcAddr == <source_ip>`，layer 为 NETWORK_FORWARD。
- 进程内 gVisor 栈处理 TCP、UDP、IPv4 分片及重传；仅复用 tun2socks core，不加载其 TUN 设备。
- TCP 使用 SOCKS CONNECT；UDP 使用 SOCKS UDP ASSOCIATE，relay 限定为本机回环地址，无直连回退。
- 转发路径上普通 TCP/UDP 53 DNS 改写到配置的公网 DNS，并经过 SOCKS；本地接收/同网段直达 DNS 不在捕获范围内。
- 回包只允许指定客户端目的地址，指定 ZeroTier 出接口；Windows 本机、Mihomo 和 ZeroTier 外层连接不匹配源地址转发过滤器。
- 独立 guard 在 proxy 退出时丢弃匹配流量；guard 退出时 proxy 保留捕获句柄并进入丢弃模式。
- 可选 SYSTEM 开机监督任务，High 优先级，等待依赖并恢复失败的子进程。见 [AUTOSTART.md](AUTOSTART.md)。

## 限制

仅 Windows amd64、IPv4 TCP/UDP；不支持 IPv6 代理或 ICMP 代理。客户端和 Windows 地址须在同一私有 `/24`；上游目前固定为 `127.0.0.1:7897`，不支持认证。

除普通未分片 DNS 外，私网、回环、链路本地和共享地址目标会被拒绝。SOCKS UDP FRAG 和域名型 UDP relay 不支持。fake-IP 地址依赖上游已有一致映射。最多256个活动流、120秒空闲超时是默认值，静默长连接可能需要重连。

送入 Mihomo 后遵循其当前模式和节点选择；规则模式下 DIRECT 仍会直连。要使用当前所选代理出口，应结合 Clash 实际模式验证客户端公网 IP。

WinDivert 转发层不与 Windows NAT 混用：活动模式拒绝任何显式 Get-NetNat 对象。不要为了绕过检查而删除不相关的 WSL/Hyper-V/NAT 配置。

guard 不是持久防火墙：双进程退出、驱动故障、重启和启动早期都没有永久阻断保证。客户端 Default Route 在安装、停止、重启与恢复 NAT 前保持 OFF。源 IP 过滤也不是设备的独立身份认证，应限制 ZeroTier 网络成员。

## 构建与配置

需要 Windows amd64 和 Go 1.26.3+；开发验证使用 Go 1.26.8。

```powershell
# 在仓库根目录执行；下载并校验官方运行库，不加载驱动、不修改网络。
& .\scripts\Fetch-Runtime.ps1
# 如下载需要现有本地代理，可加 -Proxy http://127.0.0.1:7897

go test ./... -count=1 -timeout=60s
go vet ./...
go build -trimpath -o bin/zt-gateway.exe ./cmd/zt-gateway
Copy-Item .\gateway.example.json .\gateway.json
```

`gateway.example.json` 中地址与接口编号只是示例。编辑本地 `gateway.json`，查询实际 ZeroTier IP、接口编号、转发状态，确保 SOCKS 已可用。真实配置不会被 Git 跟踪。

**当前 PowerShell NAT 运维模板仍限定 `ZeroTierNAT` 和示例 `10.147.20.0/24`。** 使用不同网段前必须审阅并调整 `scripts/Nat.ps1` 的 `$expectedPrefix` 和 `Inspect.ps1` 的 Expected 字段；仅修改 JSON 不会授权脚本管理另一条 NAT。它会拒绝前缀不匹配，不能盲目照抄启动命令。

客户端 Default Route 保持 OFF，先只读检查：

```powershell
& .\scripts\Inspect.ps1
& .\bin\zt-gateway.exe check -config .\gateway.json
```

check 校验配置、接口、驱动签名和哈希，经已有 SOCKS 测试 TCP DNS、UDP DNS 和 HTTPS 出口。它向配置 DNS 与 api.ipify.org 发送探测，不加载 WinDivert 驱动、不修改网络。

## 启动与停止

以下是对已审阅配置和专用 NAT 的手动部署路径，需要管理员授权，不会创建开机任务。

```powershell
# 先确认手机 Default Route OFF。
& .\scripts\Invoke-Operation.ps1 -Operation Start -PhoneDefaultRouteIsOff
```

启动先检查与只读驱动试运行，保存不可覆盖的原 NAT 备份，移除该专用 NAT，再启动 guard 和 proxy。返回的 ResultPath 必须为 completed / Success=true，运行状态必须 ready；确认 Windows 正常后再开启手机 Default Route，比较手机和 Mihomo 出口 IP。

如果原 NAT 有外部端口池，默认拒绝；只有你能确认从未手工添加池或映射时，才能在 Start 追加 `-ExternalPoolsAreAutomatic`。它不保证恢复临时分配的端口号。静态映射等未覆盖依赖仍会被拒绝。

```powershell
# 先关闭手机 Default Route；核验所有权后停止 proxy、guard，并恢复原 NAT。
& .\scripts\Invoke-Operation.ps1 -Operation Stop -PhoneDefaultRouteIsOff
```

失败时不会自动恢复 NAT；保留 state 和原始备份。停止完成后，核对 Get-NetNat 和保存的原参数。运维细节见 [scripts/OPERATIONS.md](scripts/OPERATIONS.md)。已安装监督任务时必须改用 [Manage.ps1](scripts/Manage.ps1)，避免任务重新拉起程序。

## 测试与隐私

本地协议测试涵盖多段 TCP、UDP、TCP/UDP DNS、重传、9KB UDP 分片、容量/过期及关闭。原环境实测了 iPhone 蜂窝 IPv4 出口和视频、SYSTEM 任务及代理退出恢复。未完成实际 Windows 重启验收、长期稳定性或所有手机 UDP 应用验收；UDP flow 计数不等同于成功回复。见 [VALIDATION.md](VALIDATION.md)。

本仓库不含真实设备配置、公网出口、运行日志、NAT 恢复备份、账号凭据、订阅、编译产物或第三方驱动二进制。日志和备份留在本机；分享排障材料前仍应手动检查。

本项目源码使用 MIT 许可证；WinDivert 等依赖保留各自许可证，见 [THIRD_PARTY.md](THIRD_PARTY.md)。
