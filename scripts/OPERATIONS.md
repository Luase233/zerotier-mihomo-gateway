# Operational scripts

`Inspect.ps1` is read-only. It reports adapter addresses, IPv4 routes and forwarding,
DNS server addresses, NAT identity, port 7897 listeners, and relevant process names.
It does not read proxy profiles, subscription URLs, credentials, process command
lines, or physical MAC addresses. `-AsJson` produces a structured snapshot; failed
queries are shown explicitly, rather than being treated as empty results.

`Nat.ps1` manages only the exact `ZeroTierNAT` with internal prefix
`10.147.20.0/24`. It never changes default routes, forwarding, firewall rules,
adapters, Clash settings, or unrelated NATs. It does not self-elevate.

| Action | Effect | Verification / reversal |
| --- | --- | --- |
| `Backup` | Queries network state and writes the original snapshot to `../state/ZeroTierNAT.original.clixml`. Existing original backups are validated and never overwritten. | Inspect the saved `CreateArguments` and `SetArguments`; no network setting changes. |
| `Remove -PhoneDefaultRouteIsOff` | Requires administrator privileges, a matching original backup, an unchanged exact-name/prefix NAT, no static mappings, and the saved external-pool policy. Removes that single NAT object. | Checks absence immediately. Use `Restore` to recreate the original configuration. |
| `Restore -PhoneDefaultRouteIsOff` | Recreates captured prefixes/routing-domain settings with `New-NetNat`, then restores timeout/filtering parameters with `Set-NetNat`. Already matching NATs are left intact. | Compares resulting configuration with backup. Newly appeared/changed unrelated NATs or duplicate prefixes stop restoration for review. |

Each action supports `-WhatIf`. The switch `-PhoneDefaultRouteIsOff` is an operator
acknowledgement; these scripts cannot remotely determine the iPhone toggle state.
Keep it OFF during NAT changes. Removing NAT interrupts any clients using that NAT.
Restoring configuration does not restore previous connection/session state.

### Automatic external pools: explicit history required

Microsoft describes `Get-NetNatExternalAddress` as external address pools; their
port ranges select outbound NAT session ports. It does not expose a reliable
automatic/manual classification flag. A 100-port block or a local IP alone is
therefore not treated as proof of automatic origin.

If the operator explicitly confirms that they ran only the stated
`New-NetNat -Name ZeroTierNAT -InternalIPInterfaceAddressPrefix 10.147.20.0/24`
command and configured no manual external pools or static mappings, on that
basis, `Invoke-Operation.ps1 -Operation Start -PhoneDefaultRouteIsOff
-ExternalPoolsAreAutomatic` can explicitly select the acknowledged automatic
pool policy. This switch propagates through Start to Nat Backup. It is not the
default for a new backup, and it cannot rewrite an existing original backup.

Schema 2 records the operator acknowledgement, its timestamp, every observed
external-address row, local IPv4 addresses, and exact original New/Set settings.
Automatic mode still refuses static mappings, explicit external prefixes,
nonlocal external addresses, and invalid port ranges. Restore recreates the
original NAT configuration and leaves pool allocation to Windows. It never
replays those snapshots with `Add-NetNatExternalAddress`; allocation IDs and
ports may differ after restoration. Current allocation rows are operational
state, not a promise of identical future sessions or reservations.

References: [Get-NetNatExternalAddress](https://learn.microsoft.com/en-us/powershell/module/netnat/get-netnatexternaladdress)
and [Add-NetNatExternalAddress](https://learn.microsoft.com/en-us/powershell/module/netnat/add-netnatexternaladdress).

Do not delete the `state` folder before the original NAT is restored and verified.
Its route/interface snapshots are diagnostic records, not instructions to restore
or replace the host routing table. If new WSL/Hyper-V networking state appears
after backup, the scripts preserve it and stop instead of removing it.

The NAT script was syntax-checked and its destructive paths were tested using
in-memory command mocks under Windows PowerShell 5.1. Those tests cover immutable
backup creation, missing operator acknowledgement, changed settings, dependent
objects, WhatIf, exact removal, complete settings restoration, idempotence,
unrelated NAT preservation, and cleanup after a failed restore. Mock tests do not
claim that real NAT changes or the gateway packet path have been exercised.

## Launch and stop lifecycle

`Start.ps1 -PhoneDefaultRouteIsOff` must run elevated after the operator turns
the iPhone default-route switch OFF. It saves a read-only `Inspect.ps1` snapshot,
runs the binary's read-only `check`, then a one-second SNIFF-only driver smoke
test, and stops if either fails. The smoke test loads the driver without blocking
or redirecting packets, and verifies bounded receive shutdown. Only then does
it save/validate the original NAT backup and remove that exact NAT. It starts a
hidden independent guard, waits up to 30 seconds for its PID-matched ready file,
then starts the hidden proxy with the guard PID and guard readiness proof file,
and waits up to 30 seconds for
proxy readiness. Separate stdout/stderr logs are under `state/runs/<run-id>/`.

The guard blocks the selected phone's forwarded IPv4 packets unless the active
proxy has already consumed them. A startup error deliberately leaves any started
guard/processes and the ownership manifest intact. There is no crash-triggered
NAT restoration: that would turn a failed proxy into a possible direct path.
Readiness confirms initialization; it does not prove phone connectivity or egress.

`Stop.ps1 -PhoneDefaultRouteIsOff` verifies recorded PID, exact executable path,
and process start time for both children before stopping either. It stops the
proxy first, then the guard, then restores the immutable NAT backup. Unknown
gateway processes, reused PIDs, or changed NAT state cause a stop for review.
Completed manifests remain in `state`; the original NAT backup is retained.

Start/Stop/Capture do not automatically request UAC. Invoke them from an administrator
PowerShell window or through `Invoke-Operation.ps1 -Operation Start|Stop|Capture`.
The latter explicitly requests one UAC elevation, starts a hidden worker, and
immediately returns a JSON object containing `ResultPath` and `TranscriptPath`.
Start/Stop also require `-PhoneDefaultRouteIsOff` on this launcher. Read the result
file until its `Status` becomes `completed`, `failed`, or `elevation-failed`; only
`completed` with `Success: true` means the operation finished successfully. The
worker writes its transcript and result under `state/operations/<operation-id>/`.
Launching successfully alone does not mean the gateway is ready. All helper
processes start with hidden windows; no startup tasks or services are installed.
The launch/stop lifecycle was additionally exercised with mocked child processes
under Windows PowerShell 5.1, including failed preflight, failed readiness,
duplicate starts, rollback order, and refusal of reused PID/executable identities.

`Capture.ps1 -Seconds 90` is a separate elevated, bounded SNIFF-only diagnostic
that creates logs under `state/captures`. It does not remove NAT, start a guard,
proxy, or block traffic. With existing NAT, a phone default-route test can still
use direct home internet; capture is not a protected gateway and is not a reason
to enable the phone's default route before the real gateway is ready.
