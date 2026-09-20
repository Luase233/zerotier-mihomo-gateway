# iPhone control / iPhone 手机控制

[简体中文 README](README.md) · [English README](README.en.md) · [Desktop / 桌面](DESKTOP.md)

## 简体中文

通过 ZeroTier 连接 Windows，在 iPhone 网页上选择已有 Clash / Mihomo 的代理节点。界面与桌面使用同一套深蓝与青绿色设计；电脑端同步显示最近 20 条命令、来源、目标节点和执行结果。

### 安装到已配置的 Windows 网关

需要 Go、已安装的网关、Windows .NET Framework 4.x，以及 Clash 已开启的本机命名管道控制接口。默认管道为 `\\.\pipe\verge-mihomo`；本项目不会修改 Clash 配置。若使用其他管道，通过安装器的 `-Pipe` 参数指定。当前不支持通过远程 HTTP 控制器连接 Clash。

```powershell
& .\scripts\Build-Mobile.ps1
& .\scripts\Build-Desktop.ps1

# 管理员 PowerShell。更新前退出旧桌面界面，停止已有手机控制任务。
& .\scripts\Install-Mobile.ps1 -GatewayRoot C:\ZeroTierGateway
& .\scripts\Install-Desktop.ps1 -GatewayRoot C:\ZeroTierGateway

# 返回普通用户会话运行。
& C:\ZeroTierGateway\bin\zerobridge-desktop.exe
```

安装器从实际 `gateway.json` 读取 Windows / 客户端 ZeroTier 地址和接口编号，默认监听端口 `8787`。只新增专用防火墙规则 `ZeroBridgeMihomoMobile`，范围限定到该程序、ZeroTier 接口、本机地址、端口和配置的客户端地址。不会修改原网关任务、NAT 或上游代理选择。第一次安装生成随机访问密钥；重新安装保留已有密钥。

手机服务任务 `ZeroBridgeMihomoMobile` 与界面任务 `ZeroBridgeMihomoTray` 均在指定用户登录时以普通权限启动。若提升权限使用另一个管理员账户，给两个安装器传入 `-UserId '计算机名\实际用户'`。手机控制需要该用户保持登录、电脑和 Clash 在线；不能把它等同于 SYSTEM 网关的开机自启。关闭桌面界面不会停止手机服务。

### iPhone 配对与使用

1. 手机连接同一 ZeroTier 网络。仅控制节点不需要开启 Enable Default Route；如需手机流量使用此网关，继续遵循原网关配置。
2. 电脑 ZeroBridge 点击「手机控制」。用 iPhone 扫描二维码，通过 Safari 打开，然后点击「连接电脑」。也可手动打开 `http://<Windows-ZeroTier-IP>:8787/` 并输入电脑显示的密钥。
3. Safari「分享 → 添加到主屏幕」。如有「作为网页 App 打开」选项，保持开启。主屏幕 App 如再次要求配对，重新扫码或输入密钥。
4. 在「代理」选择手动代理组，搜索并选择节点，确认切换。在「记录」查看结果；电脑端主面板及手机控制窗口约每 2 秒同步显示。

全局模式下通常选择 `GLOBAL`；规则模式下按实际规则使用的代理组调整。自动测速、故障转移和负载均衡组只读，避免悄悄改变其自动选择行为。嵌套组只在被实际出站路径引用时产生效果。

切换影响电脑上共享的 Clash 配置和新建连接，已有连接可能保留原节点。页面不会主动中断全部连接，不改变 Clash 模式、不添加订阅、不重启 Clash。成功表示已经回读确认选中节点，不等于验证该节点互联网可达性。

### 访问与记录

- 浏览器只访问 ZeroBridge 的有限接口：读取经过筛选的代理组/模式，以及选择手动组已有成员。Clash 的完整管理接口不暴露给手机。
- 服务仅监听明确的私有 IPv4 地址；应用还校验来源 IP、Host、Origin 与访问密钥，不信任 `X-Forwarded-For`。本机 ZeroTier 地址也加入白名单，供本机验证。
- 页面为 HTTP，由 ZeroTier 提供隧道加密，没有单独的 HTTPS 层。仅用于受信任的私有网络，不应映射到公网。主屏幕入口是在线网页，不提供离线控制或 Service Worker。
- 二维码含访问密钥；密钥通过 URL fragment 带入，页面读取后移除 fragment。勾选「在这台设备记住连接」会把密钥保存在浏览器 localStorage，否则保存在 sessionStorage。「忘记此设备」只清除当前浏览器凭据，不撤销其他设备。
- 撤销已分享密钥：停止 `ZeroBridgeMihomoMobile` 任务，删除本机 `mobile.json`，重新运行安装器生成新密钥，然后重新配对。先退出并重开桌面配对窗口以加载新二维码。不要删除 `gateway.json`。
- 本机 `mobile.json` 保存密钥和监听配置；`state/mobile/` 保存二维码、状态、日志和最多 20 条近期命令。它们全部被 Git 忽略。重启会恢复近期记录；历史记录有限，不是长期审计数据库。
- 提交命令先保存待处理记录，执行后读取 Clash 当前选择确认。超时、断连或重启导致结果不明时显示「待确认」，请刷新当前选择后再决定是否重试。

卸载手机控制时，停止并删除 `ZeroBridgeMihomoMobile` 任务，删除同名防火墙规则即可停用网络入口；原网关和桌面任务独立保留。

本项目仅提供本地转发与已有代理的控制功能，不提供 VPN、节点、订阅、账号或网络出口服务。见 [DISCLAIMER.md](DISCLAIMER.md)。

## English

Connect iPhone to Windows through ZeroTier and select nodes already configured in Clash / Mihomo. The mobile page shares the desktop's navy and teal design. The desktop shows the last twenty commands, their source, target and result.

### Install on a configured Windows gateway

Requires Go, an existing gateway installation, Windows .NET Framework 4.x and an enabled local Clash named-pipe controller. The default is `\\.\pipe\verge-mihomo`; use the installer's `-Pipe` argument for another local pipe. The project does not modify Clash configuration and does not currently connect to remote HTTP controllers.

```powershell
& .\scripts\Build-Mobile.ps1
& .\scripts\Build-Desktop.ps1

# Administrator PowerShell. Exit the old desktop and stop an existing mobile task before updating.
& .\scripts\Install-Mobile.ps1 -GatewayRoot C:\ZeroTierGateway
& .\scripts\Install-Desktop.ps1 -GatewayRoot C:\ZeroTierGateway

# Run from the ordinary user session.
& C:\ZeroTierGateway\bin\zerobridge-desktop.exe
```

The installer reads the actual addresses and interface index from `gateway.json`, defaults to port `8787`, and adds a dedicated `ZeroBridgeMihomoMobile` firewall rule scoped to the executable, ZeroTier interface, local address, port and configured client. It does not modify the original gateway task, NAT or selected proxy. First installation generates a random token; reinstalling retains it.

`ZeroBridgeMihomoMobile` and `ZeroBridgeMihomoTray` run at user logon with limited privileges. If elevation uses another administrator account, pass `-UserId 'COMPUTER\actual-user'` to both installers. Mobile control requires that user to remain logged on, with the computer and Clash available. It is separate from the SYSTEM gateway boot task. Exiting the desktop does not stop the mobile service.

### Pair and use iPhone

1. Connect to the same ZeroTier network. Proxy control alone does not require Enable Default Route; routing phone traffic through the gateway still requires the original gateway setup.
2. Click Mobile control on the desktop. Scan the QR, open in Safari and tap Connect. Alternatively open `http://<Windows-ZeroTier-IP>:8787/` and enter the displayed key.
3. In Safari choose Share → Add to Home Screen, retaining Open as Web App if offered. If the Home Screen app asks to pair again, scan or enter the key again.
4. Choose a manual selector group, search/select a node and confirm. View its result in History; the desktop summary and command window refresh about every two seconds.

In global mode, normally choose `GLOBAL`; in rule mode, change the group used by the relevant rules. Automatic latency, fallback and load-balancing groups are read-only. Nested groups matter only if the active outbound path references them.

Selections affect the computer's shared Clash configuration and new connections. Existing connections may retain their previous node. The page does not disconnect all connections, change modes, add subscriptions or restart Clash. Success means the current selection was read back and matched, not that the node's Internet connectivity was tested.

### Access and records

- The browser uses a limited API for filtered group/mode state and selecting existing members of manual groups. The complete Clash administration API is not exposed.
- The service binds an explicit private IPv4 address and checks client IP, Host, Origin and access token. It ignores forwarded-IP headers. The local ZeroTier address is also allowed for local verification.
- HTTP is carried inside ZeroTier's encrypted tunnel, without a separate HTTPS layer. Use only on a trusted private network; do not publish the port to the Internet. The Home Screen entry is online-only, without offline control or a Service Worker.
- The pairing QR contains a credential in the URL fragment, which the page removes after reading. Remember this device stores the key in localStorage; otherwise sessionStorage is used. Forget device clears only that browser's credential and does not revoke other devices.
- To revoke shared credentials, stop the `ZeroBridgeMihomoMobile` task, delete local `mobile.json`, rerun the installer and pair again. Reopen the desktop pairing window to reload the QR. Do not delete `gateway.json`.
- Local `mobile.json` contains credentials/listen settings; `state/mobile/` holds the QR, status, logs and up to twenty recent commands. All are ignored by Git. Recent records survive restart but are not a permanent audit database.
- Commands persist a pending record before execution and read back the current selection afterward. Timeouts, disconnections or restarts may leave an unconfirmed result; refresh the current selection before retrying.

To disable mobile control, stop/delete the `ZeroBridgeMihomoMobile` task and remove the firewall rule of the same name. The gateway and desktop tasks remain independent.

This project provides local forwarding and control of an existing proxy only. It supplies no VPN service, nodes, subscriptions, accounts or Internet egress service. See [DISCLAIMER.md](DISCLAIMER.md).
