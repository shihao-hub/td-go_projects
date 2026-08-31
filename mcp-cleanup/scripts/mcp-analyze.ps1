param(
    [double]$ThresholdHours = 2,
    [string]$PlanPath = "$PSScriptRoot\mcp-kill-plan.json"
)
$ErrorActionPreference = "Stop"

# ---- baseline memory ----
$os = Get-CimInstance Win32_OperatingSystem
$baseline = @{
    TotalGB = [math]::Round($os.TotalVisibleMemorySize/1MB, 2)
    FreeGB  = [math]::Round($os.FreePhysicalMemory/1MB, 2)
    UsedPct = [math]::Round(100*(1 - $os.FreePhysicalMemory/$os.TotalVisibleMemorySize), 1)
}

# ---- snapshot processes ----
$all = Get-CimInstance Win32_Process
$byId = @{}
foreach ($p in $all) { $byId[[string]$p.ProcessId] = $p }

# wrapper processes that may sit inside an MCP chain (between the real owner app and the server)
$wrappers = @("cmd.exe","node.exe","conhost.exe","npm.exe")
$now = Get-Date
$cutoff = $now.AddHours(-$ThresholdHours)

# targets: npx-launched node servers, plus cmd wrappers that launch npx (cmd /c npx ...)
# NOTE: interactive user terminals never have npx in their own command line, so they are excluded naturally
$targets = $all | Where-Object {
    ($_.Name -eq "node.exe" -and $_.CommandLine -match "npx") -or
    ($_.Name -eq "cmd.exe" -and $_.CommandLine -match "/c" -and $_.CommandLine -match "npx")
}

$rows = @()
foreach ($t in $targets) {
    # walk up the chain: through wrapper processes only
    $cur = $t; $top = $t; $rootProc = $null; $orphan = $false; $hops = 0
    while ($hops -lt 10) {
        $par = $byId[[string]$cur.ParentProcessId]
        if (-not $par) { $orphan = $true; break }
        # PID-reuse guard: a real parent is always older than its child
        if ($par.CreationDate -and $cur.CreationDate -and ($par.CreationDate -gt $cur.CreationDate)) { $orphan = $true; break }
        if ($wrappers -notcontains $par.Name) { $rootProc = $par; break }
        $cur = $par; $top = $par; $hops++
    }
    $rootName = if ($orphan) { "(dead)" } elseif ($rootProc) { $rootProc.Name } else { "(deep-chain)" }
    $rows += [PSCustomObject]@{
        Pid         = $t.ProcessId
        Name        = $t.Name
        Created     = $t.CreationDate
        AgeHrs      = [math]::Round(($now - $t.CreationDate).TotalHours, 1)
        MemMB       = [math]::Round($t.WorkingSetSize/1MB)
        Orphan      = $orphan
        Root        = $rootName
        KillTopPid  = $top.ProcessId
        KillTopName = $top.Name
        CmdLine     = $t.CommandLine
        Verdict     = ""
    }
}

# ---- rule 1: orphans (owner session gone) -> kill regardless of age ----
foreach ($r in ($rows | Where-Object { $_.Orphan })) { $r.Verdict = "orphan" }

# ---- rule 2: any attached instance older than the threshold -> kill ----
$ageTag = "old>" + $ThresholdHours + "h"
foreach ($r in ($rows | Where-Object { -not $_.Orphan -and $_.Created -lt $cutoff })) { $r.Verdict = $ageTag }

$killRows  = @($rows  | Where-Object { $_.Verdict -ne "" })
$keepRows  = @($rows  | Where-Object { $_.Verdict -eq "" })
$killTops  = @($killRows | Select-Object -ExpandProperty KillTopPid -Unique)

# ---- report ----
"=== BASELINE ==="
"Mem: {0}% used, {1} GB free / {2} GB total   (threshold: {3}h)" -f $baseline.UsedPct, $baseline.FreeGB, $baseline.TotalGB, $ThresholdHours
""
"=== TARGETS BY ROOT (all npx-related processes found) ==="
if ($rows.Count -eq 0) { "(none found)" }
$rows | Group-Object Root | Sort-Object Count -Descending | ForEach-Object {
    "{0,-22} count={1,-4} mem={2,6} MB" -f $_.Name, $_.Count, (($_.Group | Measure-Object MemMB -Sum).Sum)
}
""
"=== KILL LIST: {0} processes in {1} chains ===" -f $killRows.Count, $killTops.Count
$killRows | Sort-Object Verdict, Root, Created | ForEach-Object {
    $cl = $_.CmdLine; if ($cl.Length -gt 70) { $cl = $cl.Substring(0,70) }
    "{0,-8} {1,-9} {2,-14} age={3,6}h {4,-10} top={5} :: {6}" -f $_.Pid, $_.Name, $_.Root, $_.AgeHrs, $_.Verdict, $_.KillTopPid, $cl
}
""
"=== KEEP: {0} processes ===" -f $keepRows.Count
$keepRows | Sort-Object Root, Created | ForEach-Object {
    $cl = $_.CmdLine; if ($cl.Length -gt 70) { $cl = $cl.Substring(0,70) }
    "{0,-8} {1,-9} {2,-14} age={3,6}h top={4} :: {5}" -f $_.Pid, $_.Name, $_.Root, $_.AgeHrs, $_.KillTopPid, $cl
}
""
$estMB = [math]::Round((($killRows | Measure-Object MemMB -Sum).Sum))
"Estimated direct reclaim from node targets: {0} MB (working set only; commit + compressed store reclaim may differ)" -f $estMB

# ---- save kill plan for the execute step ----
$plan = @{
    TakenAt       = $now
    Baseline      = $baseline
    ThresholdHours= $ThresholdHours
    KillTops      = @($killTops | ForEach-Object { [int]$_ })
}
$plan | ConvertTo-Json | Set-Content -Path $PlanPath -Encoding UTF8
""
"Kill plan saved: $PlanPath"
