using System.ComponentModel;
using System.Diagnostics;

namespace Dnf90.Launcher;

internal static class BundleProcesses
{
    internal static void RequireExclusive(string app, int uiPid = -1, int corePid = -1)
    {
        app = Path.GetFullPath(app).TrimEnd(Path.DirectorySeparatorChar);
        foreach (var name in new[] { "DNF90", "DNF90.Core" })
        foreach (var process in Process.GetProcessesByName(name))
        {
            using (process)
            {
                if (process.Id == uiPid || process.Id == corePid) continue;
                string? path;
                try { path = process.MainModule?.FileName; }
                catch (Exception error) when (error is InvalidOperationException or ArgumentException or Win32Exception) { continue; }
                if (path is not null && Path.GetDirectoryName(Path.GetFullPath(path))!.Equals(app, StringComparison.OrdinalIgnoreCase))
                    throw new IOException("还有登录器进程使用运行目录，请关闭所有登录器窗口后重试。");
            }
        }
    }
}
