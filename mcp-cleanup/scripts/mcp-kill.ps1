param(
    [string]$PlanPath = "$PSScriptRoot\mcp-kill-plan.json"
)
$ErrorActionPreference = "Continue"
$plan = Get-Content $PlanPath -Raw | ConvertFrom-Json
$baseline = $plan.Baseline

# fresh snapshot for at-kill-time verification (anti PID-reuse)
$all = Get-CimInstance Win32_Process
$byId = @{}
foreach ($p in $all) { $byId[[string]$p.ProcessId] = $p }

$ok = 0; $skipped = 0; $alreadyGone = 0
foreach ($tp in $plan.KillTops) {
    $p = $byId[[string]$tp]
    if (-not $p) { $alreadyGone++; "PID $tp : already gone"; continue }
    if ($p.Name -ne "cmd.exe" -and $p.Name -ne "node.exe") { $skipped++; "SKIP PID $tp : unexpected name $($p.Name)"; continue }
    if ($p.CommandLine -notmatch "npx") { $skipped++; "SKIP PID $tp : cmdline no longer matches npx"; continue }
    $out = & taskkill.exe /PID $tp /T /F 2>&1
    if ($LASTEXITCODE -eq 0) { $ok++ } else { $skipped++; "FAIL PID $tp : $out" }
}
"killed chains=$ok  skipped=$skipped  alreadyGone=$alreadyGone"

Start-Sleep -Seconds 3

# ---- after stats ----
$os = Get-CimInstance Win32_OperatingSystem
$afterFree = [math]::Round($os.FreePhysicalMemory/1MB, 2)
$afterPct  = [math]::Round(100*(1 - $os.FreePhysicalMemory/$os.TotalVisibleMemorySize), 1)

$proc2 = Get-CimInstance Win32_Process
$cntNode = @($proc2 | Where-Object Name -eq "node.exe").Count
$cntCmd  = @($proc2 | Where-Object Name -eq "cmd.exe").Count
$cntCon  = @($proc2 | Where-Object Name -eq "conhost.exe").Count

# census of remaining npx-related processes (should all be younger than the threshold)
$byId2 = @{}
foreach ($p in $proc2) { $byId2[[string]$p.ProcessId] = $p }
$remain = @($proc2 | Where-Object {
    ($_.Name -eq "node.exe" -and $_.CommandLine -match "npx") -or
    ($_.Name -eq "cmd.exe" -and $_.CommandLine -match "/c" -and $_.CommandLine -match "npx")
})
""
"=== BEFORE ===  Mem {0}% used, {1} GB free" -f $baseline.UsedPct, $baseline.FreeGB
"=== AFTER  ===  Mem {0}% used, {1} GB free" -f $afterPct, $afterFree
"remaining: node=$cntNode  cmd=$cntCmd  conhost=$cntCon  npx-related=$($remain.Count)"
if ($remain.Count -gt 0) {
    $remain | Group-Object {
        $cur = $_; $rn = "(dead)"
        for ($i = 0; $i -lt 8; $i++) {
            $par = $byId2[[string]$cur.ParentProcessId]
            if (-not $par) { break }
            if (@("cmd.exe","node.exe","conhost.exe","npm.exe") -notcontains $par.Name) { $rn = $par.Name; break }
            $cur = $par
        }
        $rn
    } | ForEach-Object {
        $maxAge = ($_.Group | ForEach-Object { [math]::Round(((Get-Date) - $_.CreationDate).TotalHours,1) } | Measure-Object -Maximum).Maximum
        "  {0,-20} count={1,-3} maxAge={2}h" -f $_.Name, $_.Count, $maxAge
    }
}
