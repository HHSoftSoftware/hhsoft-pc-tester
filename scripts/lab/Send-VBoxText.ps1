[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [ValidateNotNullOrEmpty()]
    [string]$VmName,

    [Parameter(Mandatory = $true)]
    [AllowEmptyString()]
    [string]$Text,

    [string]$VBoxManage = 'C:\Program Files\Oracle\VirtualBox\VBoxManage.exe',

    [ValidateRange(1, 128)]
    [int]$ScanCodesPerCall = 96,

    [ValidateRange(0, 5000)]
    [int]$DelayMilliseconds = 25
)

$ErrorActionPreference = 'Stop'
if (-not (Test-Path -LiteralPath $VBoxManage -PathType Leaf)) {
    throw 'VBoxManage is unavailable.'
}

$ScanCode = @{
    '1' = 0x02; '2' = 0x03; '3' = 0x04; '4' = 0x05; '5' = 0x06
    '6' = 0x07; '7' = 0x08; '8' = 0x09; '9' = 0x0a; '0' = 0x0b
    '-' = 0x0c; '=' = 0x0d; 'q' = 0x10; 'w' = 0x11; 'e' = 0x12
    'r' = 0x13; 't' = 0x14; 'y' = 0x15; 'u' = 0x16; 'i' = 0x17
    'o' = 0x18; 'p' = 0x19; '[' = 0x1a; ']' = 0x1b; 'a' = 0x1e
    's' = 0x1f; 'd' = 0x20; 'f' = 0x21; 'g' = 0x22; 'h' = 0x23
    'j' = 0x24; 'k' = 0x25; 'l' = 0x26; ';' = 0x27; '\' = 0x2b
    'z' = 0x2c; 'x' = 0x2d; 'c' = 0x2e; 'v' = 0x2f; 'b' = 0x30
    'n' = 0x31; 'm' = 0x32; ',' = 0x33; '.' = 0x34; '/' = 0x35
    ' ' = 0x39
}
$ScanCode[[string][char]39] = 0x28
$ScanCode[[string][char]96] = 0x29

$Shifted = @{
    '!' = '1'; '@' = '2'; '#' = '3'; '$' = '4'; '%' = '5'; '^' = '6'
    '&' = '7'; '*' = '8'; '(' = '9'; ')' = '0'; '_' = '-'; '+' = '='
    '{' = '['; '}' = ']'; '|' = '\'; ':' = ';'; '<' = ','; '>' = '.'
    '?' = '/'; '~' = [char]96
}
$Shifted[[string][char]34] = [string][char]39

$Bytes = New-Object System.Collections.Generic.List[string]
foreach ($Character in $Text.ToCharArray()) {
    if ($Character -eq [char]13) {
        continue
    }
    if ($Character -eq [char]10) {
        $Bytes.Add('1c'); $Bytes.Add('9c')
        continue
    }
    if ($Character -eq [char]9) {
        $Bytes.Add('0f'); $Bytes.Add('8f')
        continue
    }

    $Lookup = [string]$Character
    $NeedsShift = $false
    if ($Lookup -cmatch '^[A-Z]$') {
        $NeedsShift = $true
        $Lookup = $Lookup.ToLowerInvariant()
    } elseif ($Shifted.ContainsKey($Lookup)) {
        $NeedsShift = $true
        $Lookup = [string]$Shifted[$Lookup]
    }
    if (-not $ScanCode.ContainsKey($Lookup)) {
        throw ('Character cannot be entered by this US-keyboard harness: U+' + ([int]$Character).ToString('X4'))
    }

    $Code = [int]$ScanCode[$Lookup]
    if ($NeedsShift) { $Bytes.Add('2a') }
    $Bytes.Add($Code.ToString('x2'))
    $Bytes.Add(($Code + 0x80).ToString('x2'))
    if ($NeedsShift) { $Bytes.Add('aa') }
}

for ($Offset = 0; $Offset -lt $Bytes.Count; $Offset += $ScanCodesPerCall) {
    $End = [Math]::Min($Offset + $ScanCodesPerCall - 1, $Bytes.Count - 1)
    & $VBoxManage controlvm $VmName keyboardputscancode @($Bytes[$Offset..$End]) | Out-Null
    if ($LASTEXITCODE -ne 0) {
        throw ('Failed to send keys to VirtualBox VM: ' + $VmName)
    }
    if ($DelayMilliseconds -gt 0) {
        Start-Sleep -Milliseconds $DelayMilliseconds
    }
}
