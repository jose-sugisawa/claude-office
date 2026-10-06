# claude-office を入れる（Windows）。clone したフォルダの PowerShell で .\install.ps1 を実行する。
#
#   .\install.ps1              入れる（実行ファイル → 自動起動 → ステータスライン → ブラウザで開く）
#   .\install.ps1 -Yes         聞かずに全部はいで進める
#   .\install.ps1 -Uninstall   外す（島の一覧などは残す）
#
# 実行が止められたら：powershell -ExecutionPolicy Bypass -File .\install.ps1
# 実行ファイルは %LOCALAPPDATA%\claude-office\claude-office.exe に置く。
param([switch]$Yes, [switch]$Uninstall)
$ErrorActionPreference = 'Stop'

$Repo = 'jose-sugisawa/claude-office'
$BinDir = Join-Path $env:LOCALAPPDATA 'claude-office'
$Bin = Join-Path $BinDir 'claude-office.exe'
$Addr = if ($env:CLAUDE_OFFICE_ADDR) { $env:CLAUDE_OFFICE_ADDR } else { '127.0.0.1:7777' }
$Here = $PSScriptRoot

function Say($m) { Write-Host $m -ForegroundColor White }
function Warn($m) { Write-Host $m -ForegroundColor Yellow }
function Ask($q) {
  if ($Yes) { return $true }
  $a = Read-Host "$q [Y/n]"
  return -not ($a -match '^(n|no|いいえ)$')
}

if ($Uninstall) {
  if (Test-Path $Bin) {
    & $Bin uninstall
    & $Bin statusline uninstall
    Remove-Item $Bin -Force -ErrorAction SilentlyContinue
    Say '外しました。島の一覧などは %USERPROFILE%\.claude\office に残っています。'
  } else { Warn "$Bin が見つかりません。" }
  exit 0
}

New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
if ((Get-Command go -ErrorAction SilentlyContinue) -and (Test-Path (Join-Path $Here 'go.mod'))) {
  Say '1/4 ビルドしています'
  Push-Location $Here
  try { go build -trimpath -ldflags '-s -w' -o $Bin . ; if ($LASTEXITCODE) { throw 'go build に失敗しました' } } finally { Pop-Location }
} else {
  $arch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'arm64' } else { 'amd64' }
  $file = "claude-office_windows_$arch.zip"
  $base = "https://github.com/$Repo/releases/latest/download"
  Say "1/4 Go が無いので、リリースから取ってきます`n    $base/$file"
  $tmp = Join-Path ([IO.Path]::GetTempPath()) ([Guid]::NewGuid())
  New-Item -ItemType Directory -Path $tmp | Out-Null
  try {
    Invoke-WebRequest "$base/$file" -OutFile (Join-Path $tmp 'c.zip')
    Invoke-WebRequest "$base/checksums.txt" -OutFile (Join-Path $tmp 'checksums.txt')
    # リリースの checksums.txt と照らし合わせる（途中で書き換えられたものや壊れたものを入れない）
    $want = Get-Content (Join-Path $tmp 'checksums.txt') | ForEach-Object { $p = $_ -split '\s+\*?'; if ($p[1] -eq $file) { $p[0] } } | Select-Object -First 1
    $got = (Get-FileHash (Join-Path $tmp 'c.zip') -Algorithm SHA256).Hash
    if (-not $want -or $want -ne $got) {
      Warn "取ってきたファイルが checksums.txt と合いません（$file）。入れずに止めます。"
      exit 1
    }
    Expand-Archive (Join-Path $tmp 'c.zip') -DestinationPath $tmp
    Get-Process claude-office -ErrorAction SilentlyContinue | Stop-Process -Force
    Copy-Item (Join-Path $tmp 'claude-office.exe') $Bin -Force
  } catch {
    Warn '取ってこられませんでした。Go（https://go.dev/dl/）を入れてから、もう一度実行してください。'
    exit 1
  } finally { Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue }
}
Write-Host "    $Bin"

Say '2/4 ログイン時に自動で起動するようにします'
& $Bin install -addr $Addr
if ($LASTEXITCODE) { Warn "自動起動の登録はできませんでした。$Bin を手で実行すれば使えます。" }

Say '3/4 Claude Code のステータスラインに登録します'
if (Ask '    %USERPROFILE%\.claude\settings.json に statusLine を足してよいですか（元の設定は settings.json.bak に残します）') {
  & $Bin statusline install
  if ($LASTEXITCODE) {
    if (Ask '    別のステータスラインを置き換えますか') { & $Bin statusline install -force }
    else { Warn '    登録しませんでした。コンテキストと使用量は出ませんが、ほかは使えます。' }
  }
}

$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (-not ($userPath -split ';' | Where-Object { $_ -eq $BinDir })) {
  if (Ask "    $BinDir を PATH に足しますか（claude-office とだけ打てば動くようになります）") {
    [Environment]::SetEnvironmentVariable('Path', "$userPath;$BinDir", 'User')
    Write-Host '    足しました。新しいターミナルから効きます。'
  }
}

Say "4/4 できました → http://$Addr"
Write-Host "`n    次からは、係名を付けて Claude Code を起動してください：`n      claude -n app@review`n    島は画面の「＋ 島を作る」から作れます。外すときは .\install.ps1 -Uninstall です。`n"
Start-Process "http://$Addr"
