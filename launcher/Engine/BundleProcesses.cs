using System.ComponentModel;
using System.Diagnostics;
using System.Runtime.InteropServices;
using Microsoft.Win32.SafeHandles;

namespace Dnf90.Launcher;

internal static class BundleProcesses
{
    [DllImport("kernel32.dll", SetLastError = true)]
    private static extern uint WaitForSingleObject(SafeProcessHandle handle, uint milliseconds);
    [DllImport("kernel32.dll", SetLastError = true)]
    private static extern SafeProcessHandle OpenProcess(uint access, bool inherit, int pid);
    [DllImport("kernel32.dll", SetLastError = true)]
    private static extern bool GetProcessTimes(SafeProcessHandle handle, out long created, out long exited, out long kernel, out long user);

    private static SafeProcessHandle OpenForWait(int pid)
    {
        // SYNCHRONIZE | QUERY_LIMITED_INFORMATION. Process.GetProcessById and
        // Process.SafeHandle can reject a process during DLL teardown because
        // its exit code is already set; open the kernel handle directly.
        var handle = OpenProcess(0x00101000, false, pid);
        if (!handle.IsInvalid) return handle;
        var error = Marshal.GetLastWin32Error();
        if (error == 87) return handle; // PID no longer exists.
        handle.Dispose();
        throw new Win32Exception(error);
    }

    private static bool FullyExited(SafeProcessHandle handle) => handle.IsInvalid || WaitForSingleObject(handle, 0) switch
    {
        0 => true,
        258 => false,
        _ => throw new Win32Exception(Marshal.GetLastWin32Error())
    };

    private static async Task WaitForCompleteExit(SafeProcessHandle handle, TimeSpan limit)
    {
        var timer = Stopwatch.StartNew();
        // ExitProcess sets the exit code before DLL_PROCESS_DETACH completes.
        // Only the kernel process signal means its files have been released.
        while (!FullyExited(handle))
        {
            if (timer.Elapsed >= limit) throw new TimeoutException("登录器进程尚未完全退出。");
            await Task.Delay(50);
        }
    }

    private static bool IsInstalled(Process process, string app)
    {
        try
        {
            var path = process.MainModule?.FileName;
            return path is not null && new[] { "DNF90.exe", "DNF90.Core.exe" }.Any(name =>
                Path.GetFullPath(path).Equals(Path.Combine(Path.GetFullPath(app), name), StringComparison.OrdinalIgnoreCase));
        }
        catch (Exception error) when (error is InvalidOperationException or ArgumentException or Win32Exception) { return false; }
    }

    internal static async Task StopInstalled(string app)
    {
        foreach (var name in new[] { "DNF90", "DNF90.Core" })
        foreach (var process in Process.GetProcessesByName(name))
        {
            using (process)
            {
                if (process.Id == Environment.ProcessId || !IsInstalled(process, app)) continue;
                using var handle = OpenForWait(process.Id);
                process.Refresh(); // Recheck the path with this PID pinned by our handle.
                if (FullyExited(handle) || !IsInstalled(process, app)) continue;
                await Stop(process, handle);
            }
        }
    }

    internal static async Task WaitForOwner(string app, int pid, long started)
    {
        if (pid <= 0) return;
        using var handle = OpenForWait(pid);
        if (FullyExited(handle)) return;
        if (!GetProcessTimes(handle, out var created, out _, out _, out _)) throw new Win32Exception(Marshal.GetLastWin32Error());
        if (DateTime.FromFileTimeUtc(created).Ticks != started) return;
        try { await WaitForCompleteExit(handle, TimeSpan.FromSeconds(3)); }
        catch (TimeoutException)
        {
            try
            {
                using var process = Process.GetProcessById(pid);
                if (!IsInstalled(process, app)) throw;
                await Stop(process, handle);
            }
            catch (ArgumentException)
            {
                // Exit code set, but a slow DLL is still detaching. Keep waiting on
                // the pinned original process, not a fresh PID or an exit-code check.
                await WaitForCompleteExit(handle, TimeSpan.FromSeconds(10));
            }
        }
    }

    // The caller owns this Process.Start handle for the replacement UI.
    internal static Task StopLaunched(Process process) => Stop(process, process.SafeHandle);

    private static async Task Stop(Process process, SafeProcessHandle handle)
    {
        try
        {
            if (FullyExited(handle)) return;
            process.CloseMainWindow();
            try { await WaitForCompleteExit(handle, TimeSpan.FromSeconds(2)); }
            catch (TimeoutException)
            {
                process.Kill(); // This exact launcher process only; never its game children.
                await WaitForCompleteExit(handle, TimeSpan.FromSeconds(10));
            }
        }
        catch (InvalidOperationException) when (FullyExited(handle)) { }
    }

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
                {
                    using var handle = OpenForWait(process.Id);
                    if (FullyExited(handle)) continue;
                    throw new IOException("还有登录器进程使用运行目录，请关闭所有登录器窗口后重试。", unchecked((int)0x80070020));
                }
            }
        }
    }
}
