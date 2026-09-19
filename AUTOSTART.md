# Optional Windows startup task

[简体中文](AUTOSTART.zh-CN.md) · **English** · [ZeroBridge Mihomo](README.en.md)

This repository is source for an experimental forwarding gateway, not a universal unattended installer or a VPN service. Review network/NAT scope in README.en.md first. Keep the client Default Route OFF during installation, stopping, rebooting and NAT restoration.

## Prepare the installation

Build and configure a reviewed copy at `C:\ZeroTierGateway`. Do not overwrite a live installation. Keep `state\ZeroTierNAT.original.clixml` and verify copied file hashes if migrating an existing deployment.

Before granting SYSTEM startup, use NTFS permissions to allow only Administrators and SYSTEM to modify the installation root and descendants. The ordinary user should have read/execute access only. This restriction must cover scripts, executable, runtime DLL/driver, configuration and state; do not schedule SYSTEM execution from an ordinary-user-writable checkout.

With the phone OFF, perform the manual checked startup in README.md, including immutable NAT backup, driver readiness and actual proxy checks. Existing WinNAT objects must not remain. Confirm `state/runtime.json` is ready. The supervisor itself will never delete/restore NAT or change routes to fix a failure.

## Register the reviewed startup task

Run in administrator Windows PowerShell after preparation above. This registers the task immediately and starts the same task for validation; it does not reboot Windows.

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

Task registration intentionally does not use `-Force`: an existing task requires inspection instead of silent replacement. High priority is applied to the supervisor and gateway children. No password is needed for SYSTEM. Network/Mihomo availability is retried; if the customized Clash core only starts after user login, client access also waits until then.

The supervisor first establishes the independent guard, then probes the upstream before launching the proxy. A proxy exit is recovered with the guard retained. If the guard exits, a replacement must report ready before the previous proxy is retired. Previous-boot manifests are archived without treating reused PIDs as owned processes. All processes are validated by image path and creation time.

## Check, stop and resume

```powershell
& 'C:\ZeroTierGateway\scripts\Manage.ps1' -Action Status
```

Normal users may see `TaskState=UnavailableWithoutElevation` because SYSTEM task metadata is protected. Runtime data remains readable; its heartbeat usually updates every five seconds. An old ready record alone does not establish live readiness. Check the actual task as administrator and verify the client exit IP after enabling the phone route.

Stop: turn the phone Default Route OFF first, then:

```powershell
& 'C:\ZeroTierGateway\scripts\Manage.ps1' -Action Stop -PhoneDefaultRouteIsOff
```

This requests UAC if needed, disables/stops the supervisor task, stops verified gateway processes, then restores the original NAT. Verify `state/manage-result.json` has completed and NAT matches the backup. Keep the phone route OFF after restoring NAT.

Resume after a completed stop: keep the phone OFF, review the original NAT's history, then use Manage Start. It does not assume external pools are automatic. Only after explicitly confirming that no pools or mappings were added manually may you append `-ExternalPoolsAreAutomatic` if required by backup validation.

```powershell
& 'C:\ZeroTierGateway\scripts\Manage.ps1' -Action Start -PhoneDefaultRouteIsOff
```

## Remove startup registration

After the completed Stop operation and verified NAT restoration, run as administrator:

```powershell
Unregister-ScheduledTask -TaskName ZeroTierGateway -Confirm:$false
```

Keep the original recovery backup until restoration is verified. Do not delete state, kill the guard alone, or restore NAT while the phone default route is enabled. The WinDivert driver may remain loaded until reboot; do not globally remove a driver that other applications might use.

Autostart is not a persistent firewall. Startup gaps, both processes exiting, driver failure and IPv6 remain outside the protection guarantee. The original deployment tested the scheduled task and recovery without rebooting Windows.
