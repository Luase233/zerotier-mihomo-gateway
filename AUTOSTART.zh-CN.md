# 可选的 Windows 开机自启

**简体中文** · [English](AUTOSTART.md) · [ZeroBridge Mihomo](README.md)

本项目是实验性转发网关，不是通用无人值守安装器，也不提供 VPN 服务。先阅读 README 的网络与 NAT 范围。安装、停止、重启及恢复 NAT 前，客户端 Default Route 保持 OFF。

## 准备安装目录

将经过审阅、构建和配置的副本放在 `C:\ZeroTierGateway`。不要覆盖正在运行的安装。迁移时保留 `state\ZeroTierNAT.original.clixml`，并核对文件哈希。

授予 SYSTEM 自启权限前，用 NTFS 权限限制安装根目录及其全部子项：仅 Administrators 和 SYSTEM 可以修改，普通用户只读和执行。保护范围包括脚本、程序、DLL/驱动、配置与 state。不要从普通用户可写的开发目录执行 SYSTEM 任务。

手机保持 OFF，按 README 完成手动预检和启动，包括不可覆盖的 NAT 备份、驱动就绪与真实代理检查。活动网关要求显式 WinNAT 对象不存在，且 `state/runtime.json` 为 ready。监督程序不会为修复启动失败而自行删除/恢复 NAT 或改路由。

## 注册开机任务

完成上述准备后，在管理员 Windows PowerShell 执行。以下命令注册任务并立即运行同一个任务进行验证，不会重启 Windows。

```powershell
$installRoot = 'C:\ZeroTierGateway'
$shellPath = Join-Path $env:SystemRoot 'System32\WindowsPowerShell\v1.0\powershell.exe'
$action = New-ScheduledTaskAction -Execute $shellPath `
  -Argument "-NoProfile -NonInteractive -ExecutionPolicy Bypass -File `"$installRoot\scripts\Supervisor.ps1`"" `
  -WorkingDirectory $installRoot
$trigger = New-ScheduledTaskTrigger -AtStartup
$principal = New-ScheduledTaskPrincipal -UserId SYSTEM -LogonType ServiceAccount -RunLevel Highest
$settings = New-ScheduledTaskSettingsSet -StartWhenAvailable `
  -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries `
  -ExecutionTimeLimit ([TimeSpan]::Zero) -MultipleInstances IgnoreNew `
  -Priority 1 -RestartCount 999 -RestartInterval (New-TimeSpan -Minutes 1)
Register-ScheduledTask -TaskName ZeroTierGateway -Action $action `
  -Trigger $trigger -Principal $principal -Settings $settings
Start-ScheduledTask -TaskName ZeroTierGateway
```

不使用 `-Force`，已有同名任务需先检查。监督进程和子进程设为 High 优先级；SYSTEM 不需要保存登录密码。网络或 Mihomo 不可用时会重试；如果定制 Clash 的内核必须登录用户后才能启动，客户端也需等到那时才能访问。

监督程序先建立独立 guard，再探测上游并启动 proxy。proxy 退出时保留 guard 进行恢复；guard 退出时，新的 guard 必须就绪后才结束旧 proxy。上次启动的记录会归档，不会把复用的 PID 误认为原进程；进程所有权使用程序路径与创建时间核验。

## 查看状态、停用与恢复使用

```powershell
& 'C:\ZeroTierGateway\scripts\Manage.ps1' -Action Status
```

普通用户可能无法查询 SYSTEM 任务，显示 `TaskState=UnavailableWithoutElevation`。运行记录仍可读取，正常情况下心跳约每5秒更新。不要仅凭长期未更新的 ready 记录认定网关正常；可用管理员权限查询正式任务状态，并在开启手机默认路由后验证出口 IP。

停止前先关闭手机 Default Route：

```powershell
& 'C:\ZeroTierGateway\scripts\Manage.ps1' -Action Stop -PhoneDefaultRouteIsOff
```

需要时弹出 UAC，随后禁用并停止监督任务，停止核验过的网关进程，最后恢复原 NAT。检查 `state/manage-result.json` 为 completed，并核对 NAT 与备份一致。恢复 NAT 后手机默认路由继续保持 OFF。

完成停止后，恢复使用时先保持手机 OFF，并审阅原 NAT 历史：

```powershell
& 'C:\ZeroTierGateway\scripts\Manage.ps1' -Action Start -PhoneDefaultRouteIsOff
```

此模板不会默认认定外部端口池由系统自动分配。只有明确确认从未手工添加池或映射后，才可在备份校验需要时追加 `-ExternalPoolsAreAutomatic`。

## 撤销开机启动

先完成 Stop 并验证 NAT 恢复，再在管理员 PowerShell 执行：

```powershell
Unregister-ScheduledTask -TaskName ZeroTierGateway -Confirm:$false
```

恢复验证完成前保留原始备份。不要单独结束 guard、删除 state，或在手机默认路由开启时恢复 NAT。WinDivert 驱动可能保留到重启，不要全局删除其他软件也可能使用的驱动。

开机自启不等于持久防火墙。启动空窗、所有进程退出、驱动故障和 IPv6 都不在永久阻断保证范围内。原部署验证了任务运行和恢复，但没有通过重启 Windows 完成验收。
