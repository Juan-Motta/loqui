#!/usr/bin/env pwsh
# goal-digest.ps1 — parity twin of goal-digest.sh. Hashes git's RAW stdout bytes.
param([string]$Base = '', [string]$RepoDir = '.', [string]$Mode = '')
$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $false
$env:LC_ALL = 'C'
function Fail($m) { [Console]::Error.WriteLine("goal-digest: $m"); exit 3 }
if ([string]::IsNullOrEmpty($Base)) { Fail 'missing base_sha' }
if (-not (Get-Command git -ErrorAction SilentlyContinue)) { Fail 'git not found' }
$repo = (Resolve-Path -LiteralPath $RepoDir).Path

# run git with raw-byte stdout captured to a file; return exit code
function GitRawTo($file, [string[]]$gitArgs) {
  $psi = [System.Diagnostics.ProcessStartInfo]::new()
  $psi.FileName = 'git'; $psi.WorkingDirectory = $repo
  $psi.RedirectStandardOutput = $true; $psi.UseShellExecute = $false
  foreach ($a in $gitArgs) { $psi.ArgumentList.Add($a) }
  $p = [System.Diagnostics.Process]::Start($psi)
  $fs = [System.IO.File]::Create($file)
  $p.StandardOutput.BaseStream.CopyTo($fs); $fs.Close(); $p.WaitForExit()
  return $p.ExitCode
}
& git -C $repo rev-parse --git-dir *> $null; if ($LASTEXITCODE -ne 0) { Fail 'not a repo' }
& git -C $repo cat-file -e "$Base^{commit}" *> $null; if ($LASTEXITCODE -ne 0) { Fail 'bad base' }

$excl = @(':(exclude).workflow/*', ':(exclude)docs/e2e/reports/*', ':(exclude)CONTINUITY.md',
          ':(exclude)docs/CHANGELOG.md', ':(exclude)VERSION')
$diff = @('-C', $repo, '-c', 'core.quotepath=false', 'diff', '--full-index', '--no-ext-diff',
          '--no-textconv', '--default-prefix', '--no-renames', '--no-color')
$out = [System.IO.Path]::GetTempFileName(); $tmpIdx = $null
try {
  if ($Mode -eq '--from-head') {
    $code = GitRawTo $out ($diff + @($Base, 'HEAD', '--', '.') + $excl)
  } else {
    $realIdx = (& git -C $repo rev-parse --git-path index).Trim()
    $idxAbs = if ([System.IO.Path]::IsPathRooted($realIdx)) { $realIdx } else { Join-Path $repo $realIdx }
    $tmpIdx = [System.IO.Path]::GetTempFileName()
    if (Test-Path -LiteralPath $idxAbs) { Copy-Item -LiteralPath $idxAbs $tmpIdx -Force } else { Remove-Item $tmpIdx -Force }
    $env:GIT_INDEX_FILE = $tmpIdx
    & git @(@('-C', $repo, 'add', '-N', '--', '.') + $excl) *> $null
    if ($LASTEXITCODE -ne 0) { Remove-Item Env:GIT_INDEX_FILE; Fail 'git add -N failed' }
    $code = GitRawTo $out ($diff + @($Base, '--', '.') + $excl)
    Remove-Item Env:GIT_INDEX_FILE
  }
  if ($code -ne 0) { Fail 'git diff failed' }
  $sha = [System.Security.Cryptography.SHA256]::Create()
  -join ($sha.ComputeHash([System.IO.File]::ReadAllBytes($out)) | ForEach-Object { $_.ToString('x2') })
} finally {
  Remove-Item $out -Force -ErrorAction SilentlyContinue
  if ($tmpIdx) { Remove-Item $tmpIdx -Force -ErrorAction SilentlyContinue }
}
