$ErrorActionPreference = "Stop"
Set-Location (Join-Path $PSScriptRoot "..")
if (Test-Path ".env") {
    Write-Host ".env already exists. Keeping your settings."
    return
}
function New-HexSecret {
    $bytes = New-Object byte[] 32
    $rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
    try { $rng.GetBytes($bytes) } finally { $rng.Dispose() }
    return -join ($bytes | ForEach-Object { $_.ToString("x2") })
}
$text = Get-Content ".env.example" -Raw
$text = $text.Replace("seatbook_local_only_2026", (New-HexSecret))
$text = $text.Replace("replace_this_local_secret_with_32_random_bytes_2026", (New-HexSecret))
[System.IO.File]::WriteAllText((Join-Path (Get-Location) ".env"), $text, (New-Object System.Text.UTF8Encoding($false)))
Write-Host "Created .env with fresh database and JWT secrets."
Write-Host "Admin: admin@seatbook.local / SeatBookDemo2026! (local demo only)"
