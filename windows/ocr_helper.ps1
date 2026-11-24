# Windows PowerShell 5.1 / WinRT OCR. No network, no temp files, no UI.
$ErrorActionPreference = 'Stop'
[Console]::InputEncoding = [System.Text.UTF8Encoding]::new($false)
[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)
$engine = $null
$initStatus = 'ocr_unavailable'
try {
    Add-Type -AssemblyName System.Runtime.WindowsRuntime
    $null = [Windows.Graphics.Imaging.BitmapDecoder,Windows.Graphics.Imaging,ContentType=WindowsRuntime]
    $null = [Windows.Graphics.Imaging.SoftwareBitmap,Windows.Graphics.Imaging,ContentType=WindowsRuntime]
    $null = [Windows.Graphics.Imaging.BitmapPixelFormat,Windows.Graphics.Imaging,ContentType=WindowsRuntime]
    $null = [Windows.Graphics.Imaging.BitmapAlphaMode,Windows.Graphics.Imaging,ContentType=WindowsRuntime]
    $null = [Windows.Media.Ocr.OcrEngine,Windows.Foundation,ContentType=WindowsRuntime]
    $null = [Windows.Media.Ocr.OcrResult,Windows.Foundation,ContentType=WindowsRuntime]
    $null = [Windows.Globalization.Language,Windows.Globalization,ContentType=WindowsRuntime]
    $asTask = [System.WindowsRuntimeSystemExtensions].GetMethods() | Where-Object {
        $_.Name -eq 'AsTask' -and $_.IsGenericMethodDefinition -and $_.GetParameters().Count -eq 1 -and $_.GetParameters()[0].ParameterType.Name -eq 'IAsyncOperation`1'
    } | Select-Object -First 1
    if ($null -eq $asTask) { throw 'WinRT async bridge unavailable' }
    function Await-Result($op, [Type]$type) {
        $task = $asTask.MakeGenericMethod($type).Invoke($null,@($op))
        [void]$task.Wait(-1)
        $task.Result
    }
    $lang = [Windows.Media.Ocr.OcrEngine]::AvailableRecognizerLanguages | Where-Object { $_.LanguageTag -like 'en*' } | Select-Object -First 1
    if ($null -ne $lang) { $engine = [Windows.Media.Ocr.OcrEngine]::TryCreateFromLanguage($lang) }
    if ($null -eq $engine) { $initStatus='ocr_language_missing' } else { $initStatus='ok' }
} catch { $initStatus='ocr_unavailable' }
while ($null -ne ($line = [Console]::ReadLine())) {
    $stream=$null; $random=$null; $bitmap=$null
    $response=@{status=$initStatus; words=@()}
    try {
        $req=ConvertFrom-Json -InputObject $line
        if ($initStatus -eq 'ok' -and $req.mode -eq 'recognize') {
            if ($req.png.Length -gt 500000) { throw 'oversize' }
            $bytes=[Convert]::FromBase64String($req.png)
            $stream=[IO.MemoryStream]::new($bytes)
            $random=[IO.WindowsRuntimeStreamExtensions]::AsRandomAccessStream($stream)
            $decoder=Await-Result ([Windows.Graphics.Imaging.BitmapDecoder]::CreateAsync($random)) ([Windows.Graphics.Imaging.BitmapDecoder])
            $bitmap=Await-Result ($decoder.GetSoftwareBitmapAsync([Windows.Graphics.Imaging.BitmapPixelFormat]::Bgra8,[Windows.Graphics.Imaging.BitmapAlphaMode]::Ignore)) ([Windows.Graphics.Imaging.SoftwareBitmap])
            $result=Await-Result ($engine.RecognizeAsync($bitmap)) ([Windows.Media.Ocr.OcrResult])
            $words=@(foreach ($l in $result.Lines) { foreach ($w in $l.Words) {
                if ($w.Text.Length -le 80) {
                    $r=$w.BoundingRect
                    @{Text=$w.Text;X=$r.X;Y=$r.Y;W=$r.Width;H=$r.Height}
                }
            } })
            $words=@($words | Select-Object -First 96)
            $response=@{status='ok';words=$words}
        }
    } catch { $response=@{status='ocr_error';words=@()} }
    finally {
        if ($null -ne $bitmap) { try { $bitmap.Dispose() } catch {} }
        if ($null -ne $random) { try { $random.Dispose() } catch {} }
        if ($null -ne $stream) { try { $stream.Dispose() } catch {} }
    }
    [Console]::WriteLine(($response | ConvertTo-Json -Depth 5 -Compress))
}
