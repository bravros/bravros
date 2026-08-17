<#
  Bravros installer (native Windows) — detect platform, download, verify,
  install, hand off.

  This file is a deliberate near line-by-line MIRROR of install.sh: same
  function names in the same order, same badge vocabulary, same trust chain.
  When you change one, change the other. The POSIX name is given in a comment
  above each function so the pairing survives future edits.

  Usage:
    irm https://install.bravros.dev/install.ps1 | iex

  Native Windows is the *degraded* tier (P-0015 D3). WSL + the POSIX installer
  is the recommended path and is what the README leads with; anything that
  shells out to bash is unavailable here. Install location mirrors POSIX
  exactly — %USERPROFILE%\.claude\bin (D10) — so docs, the component manifest
  and state.json need no per-OS branching.

  Verify this installer and the embedded public key at: https://bravros.dev/security
  (Key ID: 366384ABA1561E2A)

  ── The stdin question does NOT arise here ─────────────────────────────────
  install.sh has to rebind /dev/tty because `curl | sh` feeds the script itself
  through stdin, leaving nothing for a prompt to read. `irm ... | iex` has no
  such problem: iex compiles and runs the script inside the *current* PowerShell
  session, so the console host is still attached and Read-Host / a huh TUI read
  the real terminal. The equivalent guard here is simply asking the host whether
  it is interactive (Test-Interactive below) before handing off to the wizard.

  PowerShell 5.1 compatible: no ternaries, no null-coalescing, no
  Invoke-RestMethod -SkipCertificateCheck, nothing from PowerShell 7. Windows
  ships 5.1 in the box and that is the floor we target.
#>

# mirror of: `--force` argument parsing in install.sh. A `param()` block must
# be the script's first statement, so it lives here even though it is read
# further down. `-Force` works for direct execution (`.\install.ps1 -Force`);
# under `irm … | iex` there is no argv to bind, so $env:BRAVROS_FORCE is the
# escape hatch — same convention as $env:BRAVROS_ALLOW_BREW_SHADOW below.
param(
  [switch]$Force
)

# `$ErrorActionPreference = 'Stop'` is the mirror of `set -e`. There is no clean
# mirror of `set -u` that is safe under `iex` (Set-StrictMode leaks into the
# caller's session), so every variable below is explicitly initialised instead.
$ErrorActionPreference = 'Stop'

# PowerShell 5.1 still negotiates TLS 1.0 by default on older builds; GitHub
# refuses anything below 1.2, and the failure surfaces as a bare
# "underlying connection was closed" that looks like a network outage.
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

# ── Minisign public key ──────────────────────────────────────────────────────
# Identical to install.sh:MINISIGN_PUBKEY. To verify this key is genuine,
# check https://bravros.dev/security
$MinisignPubkey = 'RWQqHlahq4RjNnCasO/8yMsgtLGfdHejILKMxxpsulIs1rII6IgMO26G'

# ── Constants ────────────────────────────────────────────────────────────────
$GithubRepo = 'bravros/bravros'
$BaseUrl    = "https://github.com/$GithubRepo/releases/latest/download"
$BinDir     = Join-Path $env:USERPROFILE '.claude\bin'
$Uninstaller = Join-Path $BinDir 'bravros-uninstall.ps1'

# mirror of: FORCE parsing in install.sh
$script:ForceInstall = $false
if ($Force) { $script:ForceInstall = $true }
if ($env:BRAVROS_FORCE) { $script:ForceInstall = $true }

# Pinned minisign bootstrap — see Ensure-Minisign for the decision and its cost.
$MinisignVersion = '0.12'
$MinisignUrl     = "https://github.com/jedisct1/minisign/releases/download/$MinisignVersion/minisign-$MinisignVersion-win64.zip"
$MinisignSha256  = '37B600344E20C19314B2E82813DB2BFDCC408B77B876F7727889DBD46D539479'

# ── TTY gate ─────────────────────────────────────────────────────────────────
# Mirror of install.sh's `[ ! -t 1 ]` block. Gates colour and the spinner.
$NoTty = $false
if (-not [Environment]::UserInteractive) { $NoTty = $true }
if ($Host.Name -eq 'Default Host')       { $NoTty = $true }
if ($env:NO_COLOR)                       { $NoTty = $true }
try { $null = $Host.UI.RawUI.WindowSize } catch { $NoTty = $true }

# ── Helpers ──────────────────────────────────────────────────────────────────
# ANSI badges in install.sh; -BackgroundColor here, which is the PowerShell 5.1
# equivalent that works in conhost as well as Windows Terminal.

# mirror of: info()
function Write-InfoBadge([string]$Message) {
  if ($NoTty) { Write-Host "INFO $Message"; return }
  Write-Host ' INFO ' -BackgroundColor DarkBlue -ForegroundColor White -NoNewline
  Write-Host " $Message"
}

# mirror of: ok()
function Write-OkBadge([string]$Message) {
  if ($NoTty) { Write-Host "OK   $Message"; return }
  Write-Host '  OK  ' -BackgroundColor DarkGreen -ForegroundColor White -NoNewline
  Write-Host " $Message"
}

# mirror of: warn()
function Write-WarnBadge([string]$Message) {
  if ($NoTty) { Write-Warning $Message; return }
  Write-Host ' WARN ' -BackgroundColor DarkRed -ForegroundColor White -NoNewline
  Write-Host " $Message"
}

# mirror of: die()
# `throw`, not `exit`: under `irm | iex` the script shares the user's session,
# and `exit` would close their shell. Main catches and reports.
function Invoke-Die([string]$Message) {
  throw $Message
}

# mirror of: require()
function Test-Require([string]$Name) {
  if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
    Invoke-Die "Required tool not found: $Name"
  }
}

# mirror of: spinner_frame()
function Get-SpinnerFrame([int]$Index) {
  $frames = @([char]0x280B, [char]0x2819, [char]0x2839, [char]0x2838, [char]0x283C,
              [char]0x2834, [char]0x2826, [char]0x2827, [char]0x2807, [char]0x280F)
  return $frames[$Index % 10]
}

# mirror of: spin()
# Runs the scriptblock as a job so the console can animate. On failure the
# captured output is replayed, exactly like install.sh replays $spin_log.
#
# Arguments travel through -ArgumentList / param(), NOT `$using:`. A job runs in
# its own runspace so `$using:` works there — but the $NoTty branch invokes the
# same scriptblock inline, where `$using:` is a hard error ("A Using variable
# cannot be retrieved"). One calling convention that works in both is the only
# way this stays a single code path.
function Invoke-WithSpinner([string]$Message, [scriptblock]$Action, [object[]]$ArgumentList = @()) {
  if ($NoTty) {
    Write-InfoBadge $Message
    & $Action @ArgumentList
    return
  }
  $job = Start-Job -ScriptBlock $Action -ArgumentList $ArgumentList
  $i = 0
  while ($job.State -eq 'Running') {
    Write-Host ("`r  {0} {1}" -f (Get-SpinnerFrame $i), $Message) -NoNewline
    $i++
    Start-Sleep -Milliseconds 100
  }
  Write-Host ("`r" + (' ' * ($Message.Length + 6)) + "`r") -NoNewline
  $failed = ($job.State -eq 'Failed')
  $output = Receive-Job $job -ErrorAction SilentlyContinue 2>&1
  Remove-Job $job -Force
  if ($failed) {
    if ($output) { $output | Out-String | Write-Host }
    Invoke-Die $Message.TrimEnd('.')
  }
}

# mirror of: ensure_minisign()
#
# ── Windows minisign strategy — DECIDED HERE (P-0015 Phase 5) ───────────────
# Two options were on the table:
#
#   (a) Pinned minisign.exe download  <-- CHOSEN
#       Fetch jedisct1/minisign's official win64 release at a pinned version and
#       check it against a SHA-256 pinned in this script, then use it to verify
#       checksums.txt exactly as install.sh does.
#
#   (b) Go-binary bootstrap
#       Download bravros.exe first and have it verify its own release manifest
#       with the pubkey compiled into it.
#
# (b) is REJECTED because it is circular: the binary attesting to the archive is
# the very artifact under suspicion, so a tampered download would cheerfully
# approve itself. That is not a trust chain, it is a mirror.
#
# The cost of (a), stated plainly: it adds a bootstrap dependency on a
# third-party GitHub release, and the pinned hash must be bumped by hand on
# every minisign upgrade (a stale pin fails closed — the install aborts rather
# than skipping verification, which is the correct direction to fail). We accept
# a manual version bump in exchange for keeping the exact same Ed25519 chain on
# all three platforms. An already-installed minisign (scoop/winget/PATH) is
# preferred when present, mirroring install.sh's brew/apt branch.
function Ensure-Minisign {
  $existing = Get-Command minisign -ErrorAction SilentlyContinue
  if ($existing) { return $existing.Source }

  Write-InfoBadge 'Installing minisign (needed to verify the download)...'

  $tmpZip = Join-Path $script:TmpDir 'minisign.zip'
  $tmpDir = Join-Path $script:TmpDir 'minisign'
  Invoke-WebRequest -Uri $MinisignUrl -OutFile $tmpZip -UseBasicParsing

  $actual = (Get-FileHash -Path $tmpZip -Algorithm SHA256).Hash.ToUpperInvariant()
  if ($actual -ne $MinisignSha256) {
    Invoke-Die "minisign download failed its pinned checksum (expected $MinisignSha256, got $actual) — refusing to continue unverified."
  }

  Expand-Archive -Path $tmpZip -DestinationPath $tmpDir -Force
  # The official zip ships both slices: minisign-win64\{x86_64,aarch64}\minisign.exe
  $slice = 'x86_64'
  if ($script:Arch -eq 'arm64') { $slice = 'aarch64' }
  $exe = Join-Path $tmpDir "minisign-win64\$slice\minisign.exe"
  if (-not (Test-Path $exe)) {
    # Fall back to whatever the archive actually contains rather than guessing.
    $found = Get-ChildItem -Path $tmpDir -Filter 'minisign.exe' -Recurse | Select-Object -First 1
    if (-not $found) { Invoke-Die 'minisign.exe not found inside the downloaded archive.' }
    $exe = $found.FullName
  }
  return $exe
}

# mirror of: detect_brew_managed()
# Homebrew has no Windows equivalent, but scoop and winget can both own a
# bravros.exe, and the shadowing failure is identical: two binaries on PATH and
# no way for the user to tell which one ran.
function Test-PackageManaged {
  if ($env:BRAVROS_ALLOW_BREW_SHADOW) { return }

  $manager = ''
  if (Get-Command scoop -ErrorAction SilentlyContinue) {
    $scoopList = (& scoop list 2>$null | Out-String)
    if ($scoopList -match '(?m)^\s*bravros\s') { $manager = 'scoop' }
  }
  if (-not $manager -and (Get-Command winget -ErrorAction SilentlyContinue)) {
    $wingetList = (& winget list --id bravros 2>$null | Out-String)
    if ($wingetList -match 'bravros') { $manager = 'winget' }
  }
  if (-not $manager) { return }

  Write-WarnBadge "bravros is already installed via $manager."
  Write-Host ''
  Write-Host "  Installing into $BinDir would shadow it — whichever comes first on"
  Write-Host '  PATH would win, silently. Pick one:'
  Write-Host ''
  Write-Host "    Upgrade the $manager copy:   $manager upgrade bravros"
  Write-Host "    Or switch to this installer: $manager uninstall bravros, then re-run this script"
  Write-Host ''
  Write-Host '  (override with $env:BRAVROS_ALLOW_BREW_SHADOW=1 if you know what you are doing)'
  Invoke-Die "refusing to shadow the $manager-managed install"
}

# mirror of: profile_candidates()
# Windows has no rc-file convention for PATH — the user environment block is the
# real mechanism — but the PowerShell profile is still where a user expects to
# find such a line, so it is what we name in the manual fallback.
function Get-ProfileCandidates {
  return @($PROFILE.CurrentUserAllHosts, $PROFILE.CurrentUserCurrentHost)
}

# mirror of: path_line_for()
function Get-PathLineFor([string]$Target) {
  return ('$env:Path = "' + $BinDir + ';$env:Path"')
}

# mirror of: add_to_path()
# The durable half is SetEnvironmentVariable(..., 'User'), which survives
# reboots; updating $env:Path makes the binary usable in THIS session without a
# restart. php.new does both for the same reason. Always ends by printing the
# exact line, same as the POSIX side.
function Add-ToPath {
  $userPath = [System.Environment]::GetEnvironmentVariable('Path', 'User')
  if ($null -eq $userPath) { $userPath = '' }

  $alreadyUser = $false
  foreach ($entry in $userPath.Split(';')) {
    if ($entry.TrimEnd('\') -eq $BinDir.TrimEnd('\')) { $alreadyUser = $true }
  }

  if ($alreadyUser) {
    Write-OkBadge "$BinDir is already on the user PATH"
  } else {
    $newPath = $BinDir
    if ($userPath -ne '') { $newPath = "$BinDir;$userPath" }
    try {
      [System.Environment]::SetEnvironmentVariable('Path', $newPath, 'User')
      Write-OkBadge 'User PATH updated (new terminals will pick it up)'
    } catch {
      Write-WarnBadge 'Could not update the user PATH automatically.'
    }
  }

  # Current session, so the very next command works.
  $inSession = $false
  foreach ($entry in $env:Path.Split(';')) {
    if ($entry.TrimEnd('\') -eq $BinDir.TrimEnd('\')) { $inSession = $true }
  }
  if (-not $inSession) { $env:Path = "$BinDir;$env:Path" }

  Write-Host ''
  Write-Host '  Add this line to your PowerShell profile if the command is not found:'
  Write-Host ("    " + (Get-PathLineFor $PROFILE))
  Write-Host ("    (your profile: $PROFILE)")
  Write-Host ''
}

# mirror of: write_uninstaller()
# Removes only what this script created. Skills, templates and settings belong
# to `bravros setup` and are listed, never deleted behind the user's back.
function Write-Uninstaller {
  $body = @'
# Generated by the Bravros installer. Removes the binary this installer placed.
$ErrorActionPreference = 'Stop'
$BinDir = Join-Path $env:USERPROFILE '.claude\bin'
Write-Host "Removing $BinDir\bravros.exe..."
Remove-Item -Force -ErrorAction SilentlyContinue (Join-Path $BinDir 'bravros.exe')
Write-Host ''
Write-Host 'Removed the bravros binary.'
Write-Host ''
Write-Host 'Not removed (yours to keep or delete):'
Write-Host '  %USERPROFILE%\.claude\skills      installed skills'
Write-Host '  %USERPROFILE%\.claude\templates   installed templates'
Write-Host '  %USERPROFILE%\.claude\settings.json  (Bravros hook entries are marked as managed)'
Write-Host ''
Write-Host 'Also remove the install dir from your user PATH:'
Write-Host '  [System.Environment]::SetEnvironmentVariable("Path", (([System.Environment]::GetEnvironmentVariable("Path","User").Split(";") | Where-Object { $_ -ne $BinDir }) -join ";"), "User")'
Write-Host ''
Remove-Item -Force -ErrorAction SilentlyContinue (Join-Path $BinDir 'bravros-uninstall.ps1')
'@
  Set-Content -Path $Uninstaller -Value $body -Encoding UTF8
}

# mirror of: resolve_latest_tag()
# Same technique as install.sh: follow the redirect from .../releases/latest
# and read the tag off the final resolved URL, instead of calling
# api.github.com (keeps this script off the unauthenticated 60/hr API rate
# limit). Returns the tag string (e.g. "v3.51.0") or $null on any failure —
# callers must treat that as "skip the check, fall back to always-download".
function Resolve-LatestTag {
  try {
    $resp = Invoke-WebRequest -Uri "https://github.com/$GithubRepo/releases/latest" -Method Head -UseBasicParsing -ErrorAction Stop
    $finalUri = $resp.BaseResponse.ResponseUri.AbsoluteUri
    if (-not $finalUri) { return $null }
    $tag = $finalUri.TrimEnd('/').Split('/')[-1]
    if (-not $tag -or $tag -eq 'latest') { return $null }
    return $tag
  } catch {
    return $null
  }
}

# mirror of: detect_installed_version()
# Checks PATH first, then the install destination. `bravros version` prints
# "bravros vX.Y.Z"; this returns just "vX.Y.Z", or $null when no usable
# binary is found.
function Get-InstalledVersion {
  $candidate = $null
  $onPath = Get-Command bravros -ErrorAction SilentlyContinue
  if ($onPath) {
    $candidate = $onPath.Source
  } elseif (Test-Path (Join-Path $BinDir 'bravros.exe')) {
    $candidate = Join-Path $BinDir 'bravros.exe'
  }
  if (-not $candidate) { return $null }
  try {
    $output = & $candidate version 2>$null
    if (-not $output) { return $null }
    $first = ($output | Select-Object -First 1).ToString()
    $parts = $first.Split(' ')
    if ($parts.Count -lt 2) { return $null }
    return $parts[1]
  } catch {
    return $null
  }
}

# ── Main ─────────────────────────────────────────────────────────────────────
# mirror of: main()
function Invoke-Main {
  # Platform detection. PROCESSOR_ARCHITECTURE reports the architecture of the
  # *process*, so a 32-bit PowerShell on ARM64 says AMD64/x86; ARCHITEW6432 is
  # the machine's real answer and wins when present.
  $rawArch = $env:PROCESSOR_ARCHITECTURE
  if ($env:PROCESSOR_ARCHITEW6432) { $rawArch = $env:PROCESSOR_ARCHITEW6432 }

  switch ($rawArch.ToUpperInvariant()) {
    'AMD64' { $script:Arch = 'amd64' }
    'ARM64' { $script:Arch = 'arm64' }
    default { Invoke-Die "Unsupported architecture: $rawArch (bravros ships windows/amd64 and windows/arm64)" }
  }

  # GoReleaser emits zip for windows and tar.gz elsewhere; PowerShell 5.1 has no
  # native tar.gz reader, which is exactly why the format_overrides block exists.
  $archive = "bravros-windows-$($script:Arch).zip"

  # Version check (skipped entirely by -Force / $env:BRAVROS_FORCE). Any
  # failure to resolve the latest tag or detect a local version falls through
  # to the always-download behavior below — an installer must never brick on
  # this.
  if (-not $script:ForceInstall) {
    $latestTag = Resolve-LatestTag
    if ($latestTag) {
      $currentVersion = Get-InstalledVersion
      if ($currentVersion) {
        if ($currentVersion -eq $latestTag) {
          # mirror of: install.sh already-current handoff. No download — but
          # re-running the installer stays the repair path: still hand off to
          # `bravros setup` (idempotent, reports ALREADY CORRECT / CHANGED /
          # SKIPPED per component) instead of silently no-oping.
          Write-OkBadge "already current ($currentVersion) - no download needed"
          $setupBin = Join-Path $BinDir 'bravros.exe'
          if (-not (Test-Path $setupBin)) {
            $onPath = Get-Command bravros -ErrorAction SilentlyContinue
            if ($onPath) { $setupBin = $onPath.Source } else { $setupBin = $null }
          }
          if ($setupBin -and -not $NoTty) {
            & $setupBin setup
            return
          }
          if ($setupBin) {
            Write-Host "  Binary already current; run '$setupBin setup' to verify or repair components."
          }
          return
        }
        Write-InfoBadge "updating $currentVersion -> $latestTag"
      }
    }
  }

  Test-PackageManaged

  $script:TmpDir = Join-Path ([System.IO.Path]::GetTempPath()) ("bravros-" + [System.Guid]::NewGuid().ToString('N'))
  New-Item -ItemType Directory -Path $script:TmpDir -Force | Out-Null

  try {
    $minisign = Ensure-Minisign

    Write-Host ''
    Write-Host '  Bravros installer'
    Write-Host ''

    $archivePath   = Join-Path $script:TmpDir $archive
    $checksumsPath = Join-Path $script:TmpDir 'checksums.txt'
    $sigPath       = Join-Path $script:TmpDir 'checksums.txt.minisig'

    # Spinner jobs run in a separate runspace: nothing is inherited, so the URLs
    # and destinations are passed explicitly and TLS 1.2 is re-asserted inside.
    Invoke-WithSpinner "Downloading bravros for windows/$($script:Arch)..." {
      param($UrlArchive, $ArchivePath, $UrlSums, $SumsPath, $UrlSig, $SigPath)
      $ErrorActionPreference = 'Stop'
      [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
      Invoke-WebRequest -Uri $UrlArchive -OutFile $ArchivePath   -UseBasicParsing
      Invoke-WebRequest -Uri $UrlSums    -OutFile $SumsPath      -UseBasicParsing
      Invoke-WebRequest -Uri $UrlSig     -OutFile $SigPath       -UseBasicParsing
    } @(
      "$BaseUrl/$archive",            $archivePath,
      "$BaseUrl/checksums.txt",       $checksumsPath,
      "$BaseUrl/checksums.txt.minisig", $sigPath
    )
    Write-OkBadge "Downloaded $archive"

    # The trust chain, identical to install.sh: minisign verifies checksums.txt
    # against the pinned pubkey, then SHA-256 ties the archive to that signed
    # manifest. Every download stays verified.
    Write-InfoBadge 'Verifying minisign signature...'
    # Native-command stderr is merged and inspected by EXIT CODE, never by
    # whether anything landed on stderr. Under PowerShell 7 with
    # $ErrorActionPreference='Stop', a native tool writing to stderr can throw
    # on its own — minisign prints progress there — so the preference is relaxed
    # across this one call and the exit code is the sole verdict.
    $prevEap = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    $sigOutput = & $minisign -Vm $checksumsPath -P $MinisignPubkey -x $sigPath 2>&1
    $sigRc = $LASTEXITCODE
    $ErrorActionPreference = $prevEap
    if ($sigRc -ne 0) {
      Write-Host ($sigOutput | Out-String)
      Invoke-Die 'Signature verification failed — download may be tampered!'
    }
    Write-OkBadge 'Signature valid'

    Write-InfoBadge 'Verifying SHA256 checksum...'
    $expected = ''
    foreach ($line in (Get-Content $checksumsPath)) {
      # checksums.txt is `<sha256>  <filename>` — match the filename exactly so
      # bravros-windows-amd64.zip never matches a longer neighbouring name.
      $parts = $line -split '\s+', 2
      if ($parts.Count -eq 2 -and $parts[1].Trim() -eq $archive) { $expected = $parts[0].Trim() }
    }
    if (-not $expected) { Invoke-Die "checksums.txt has no entry for $archive" }
    $actual = (Get-FileHash -Path $archivePath -Algorithm SHA256).Hash
    if ($actual.ToUpperInvariant() -ne $expected.ToUpperInvariant()) {
      Invoke-Die 'SHA256 mismatch — download may be corrupted!'
    }
    Write-OkBadge 'Checksum verified'

    Write-InfoBadge "Installing to $BinDir\bravros.exe..."
    New-Item -ItemType Directory -Path $BinDir -Force | Out-Null
    $extractDir = Join-Path $script:TmpDir 'extract'
    Expand-Archive -Path $archivePath -DestinationPath $extractDir -Force
    $binary = Get-ChildItem -Path $extractDir -Filter 'bravros.exe' -Recurse | Select-Object -First 1
    if (-not $binary) { Invoke-Die 'bravros.exe not found inside the downloaded archive.' }
    # Copy over a running binary fails with "file in use"; move the old one aside
    # first, which Windows permits even while it is executing.
    $target = Join-Path $BinDir 'bravros.exe'
    if (Test-Path $target) {
      $stale = "$target.old"
      Remove-Item -Force -ErrorAction SilentlyContinue $stale
      Move-Item -Force $target $stale
    }
    Copy-Item -Force $binary.FullName $target
    Write-Uninstaller
    Write-OkBadge 'Installed'

    Add-ToPath

    # Hand off. `bravros setup` owns every question and every write into the
    # user's home; this script deliberately knows nothing about them. There is
    # no exec() on Windows, so we invoke and propagate rather than replace.
    if (-not $NoTty) {
      & $target setup
      return
    }

    Write-Host ''
    Write-Host 'Bravros installed. No interactive host, so the setup wizard was skipped.'
    Write-Host ''
    Write-Host '  Run it when you are at a prompt:'
    Write-Host ''
    Write-Host "    $target setup"
    Write-Host ''
    Write-Host '  Or non-interactively:'
    Write-Host ''
    Write-Host "    $target setup --all --yes"
    Write-Host ''
    Write-Host '  Then, inside a git repository:'
    Write-Host ''
    Write-Host '    cd C:\path\to\your-project'
    Write-Host '    bravros init'
    Write-Host ''
    Write-Host "  To uninstall: $Uninstaller"
    Write-Host ''
  }
  finally {
    Remove-Item -Recurse -Force -ErrorAction SilentlyContinue $script:TmpDir
  }
}

# mirror of: the `main < /dev/tty` dispatch at the bottom of install.sh.
# No tty rebinding is needed (see the header note); the only job here is to turn
# a thrown error into a clean red line instead of a PowerShell stack trace, and
# to avoid `exit` killing the session this script was iex'd into.
$script:Arch = ''
$script:TmpDir = ''
try {
  Invoke-Main
}
catch {
  if ($NoTty) {
    Write-Host "FAIL $($_.Exception.Message)"
  } else {
    Write-Host ' FAIL ' -BackgroundColor DarkRed -ForegroundColor White -NoNewline
    Write-Host " $($_.Exception.Message)"
  }
  $global:LASTEXITCODE = 1
}
