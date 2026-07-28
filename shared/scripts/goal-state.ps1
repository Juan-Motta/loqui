#!/usr/bin/env pwsh
# goal-state.ps1 — parity twin of goal-state.sh (§4/§6.2/§10), section-scoped, CRLF-tolerant.
param([string]$Cmd = '', [string]$A2 = '', [string]$A3 = '')
$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $false
function DefaultFile($f) { if ($f) { $f } else { '.workflow/state.md' } }
function Section([string]$header, [string]$file) {
  $in = $false
  foreach ($raw in Get-Content -LiteralPath $file) {
    $line = $raw -replace "`r$", ''
    if ($line -eq "## $header") { $in = $true; continue } elseif ($line -like '## *') { $in = $false }
    if ($in) { $line }
  }
}
switch ($Cmd) {
  'field' {
    $name = $A2; $file = DefaultFile $A3; if (-not (Test-Path -LiteralPath $file)) { return }
    foreach ($l in Section '/goal loop' $file) {
      if ($l -match "^\|\s*$([regex]::Escape($name))\s*\|\s*(.*?)\s*\|") { $matches[1]; break }
    }
  }
  'round-count' {
    $loop = $A2; $file = DefaultFile $A3; if (-not (Test-Path -LiteralPath $file)) { '0'; break }
    (Section 'Review log' $file | Where-Object { $_ -match "loop=$loop .*kind=round" }).Count
  }
  'ship-red-count' {
    $file = DefaultFile $A2; if (-not (Test-Path -LiteralPath $file)) { '0'; break }
    $ns = Section 'Attempts' $file | ForEach-Object { if ($_ -match 'ATTEMPT ship-red — n=(\d+)') { [int]$matches[1] } }
    if ($ns) { $ns[-1] } else { '0' }
  }
  'ship-red-bump' {
    $file = DefaultFile $A2
    $cur = [int](& pwsh -NoProfile -File $PSCommandPath 'ship-red-count' $file)
    $next = $cur + 1; $ts = (Get-Date).ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ssZ')
    $line = "- ATTEMPT ship-red — n=$next — ts=$ts"
    if (-not (Test-Path -LiteralPath $file)) { New-Item -ItemType File -Path $file | Out-Null }
    $content = @(Get-Content -LiteralPath $file)
    if ($content -contains '## Attempts') {
      $outLines = New-Object System.Collections.Generic.List[string]; $insec = $false
      foreach ($l in $content) {
        if (($l -like '## *') -and $insec) { $outLines.Add($line); $insec = $false }
        $outLines.Add($l)
        if ($l -eq '## Attempts') { $insec = $true }
      }
      if ($insec) { $outLines.Add($line) }
      Set-Content -LiteralPath $file -Value $outLines
    } else { Add-Content -LiteralPath $file -Value "`n## Attempts`n$line" }
    $next
  }
  default { [Console]::Error.WriteLine("goal-state: unknown subcommand '$Cmd'"); exit 3 }
}
