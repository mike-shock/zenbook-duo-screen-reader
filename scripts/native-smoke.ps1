$ErrorActionPreference = 'Stop'
$project = Split-Path $PSScriptRoot -Parent
Add-Type @'
using System;
using System.Collections.Generic;
using System.Runtime.InteropServices;
using System.Text;
public class DuoWindows {
 public delegate bool EnumProc(IntPtr window, IntPtr param);
 [DllImport("user32.dll")] static extern bool EnumWindows(EnumProc cb, IntPtr p);
 [DllImport("user32.dll")] static extern uint GetWindowThreadProcessId(IntPtr h, out uint pid);
 [DllImport("user32.dll")] static extern bool IsWindowVisible(IntPtr h);
 [DllImport("user32.dll", CharSet=CharSet.Unicode)] static extern int GetWindowText(IntPtr h, StringBuilder text, int length);
 [DllImport("user32.dll")] static extern bool GetWindowRect(IntPtr h, out Rect rect);
 [StructLayout(LayoutKind.Sequential)] public struct Rect {public int Left,Top,Right,Bottom;}
 public static string[] Visible(uint process) {
  var result = new List<string>();
  EnumWindows((h,p)=>{uint pid;GetWindowThreadProcessId(h,out pid);if(pid==process && IsWindowVisible(h)) {var text=new StringBuilder(256);GetWindowText(h,text,256);Rect r;GetWindowRect(h,out r);result.Add(text.ToString()+" ["+r.Left+","+r.Top+","+r.Right+","+r.Bottom+"]");}return true;},IntPtr.Zero);
  return result.ToArray();
 }
}
'@
$arguments = '-books "{0}\books" -data "{0}\dist\native-smoke-data"' -f $project
$process = Start-Process -FilePath "$project/dist/DuoReader.exe" -ArgumentList $arguments -PassThru -RedirectStandardError "$project/dist/native-stderr.log" -RedirectStandardOutput "$project/dist/native-stdout.log"
try {
 $visible = @()
 for ($i=0; $i -lt 30; $i++) {
  Start-Sleep -Seconds 1
  $process.Refresh()
  if ($process.HasExited) { throw "Reader exited early: $($process.ExitCode). See dist/native-stderr.log" }
  $visible = @([DuoWindows]::Visible($process.Id) | Where-Object { $_ -like 'Duo Reader*' })
  if ($visible.Count -gt 0) { break }
 }
 if ($visible.Count -lt 1) { throw 'No visible reader windows' }
 $visible | ForEach-Object { Write-Output $_ }
 Write-Output 'PASS: Windows host created visible reader windows.'
} finally {
 $process.Refresh()
 if (-not $process.HasExited) {
  [void]$process.CloseMainWindow()
  if (-not $process.WaitForExit(5000)) { Stop-Process -Id $process.Id }
 }
}
