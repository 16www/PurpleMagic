using System.ComponentModel;
using System.Diagnostics;
using System.Runtime.InteropServices;
using System.Security.Cryptography;
using System.Text;
using Microsoft.Win32.SafeHandles;

[assembly: System.Runtime.CompilerServices.InternalsVisibleTo("Launcher.Tests")]

namespace ServerS5A3;

// The inspected v56 startup builds this name and returns false on ERROR_ALREADY_EXISTS.
// This compatibility action is limited to one verified client, never system-wide handles.
public static class NativeClientGate
{
    private const string SupportedHash = "BC613E368E8FD49C50C35163F2ED8A28564FC23342701A5A6C22B839F3BF6B7A";
    private const string GateName = "dbefeuate_ccen_khxfor_lcar_blr";

    public static int PrepareSecondClient(Process existing, string executable, string expectedHash)
    {
        if (!OperatingSystem.IsWindows()) throw new PlatformNotSupportedException();
        if (!expectedHash.Equals(SupportedHash, StringComparison.OrdinalIgnoreCase))
            throw new InvalidDataException("此客户端版本尚未验证双开兼容性。");
        using var file = File.OpenRead(executable);
        if (!Convert.ToHexString(SHA256.HashData(file)).Equals(SupportedHash, StringComparison.OrdinalIgnoreCase))
            throw new InvalidDataException("客户端文件已改变，请恢复匹配版本后重试。");
        return ReleaseExactMutex(existing.Id, existing.StartTime.ToUniversalTime().ToFileTimeUtc(), executable, GateName);
    }

    internal static int ReleaseExactMutex(int pid, long expectedStart, string executable, string mutexName)
    {
        using var process = OpenProcess(0x440, false, pid); // QUERY_INFORMATION | DUP_HANDLE
        if (process.IsInvalid) throw new Win32Exception(Marshal.GetLastWin32Error());
        var path = new StringBuilder(32768); var length = path.Capacity;
        if (!QueryFullProcessImageName(process, 0, path, ref length) ||
            !GetProcessTimes(process, out var started, out _, out _, out _))
            throw new Win32Exception(Marshal.GetLastWin32Error());
        if (started != expectedStart || !Path.GetFullPath(path.ToString()).Equals(Path.GetFullPath(executable), StringComparison.OrdinalIgnoreCase))
            throw new InvalidDataException("正在运行的客户端与所选目录不一致，请先关闭游戏再重试。");
        if (!ProcessIdToSessionId((uint)pid, out var session)) throw new Win32Exception(Marshal.GetLastWin32Error());
        if (!ProcessIdToSessionId((uint)Environment.ProcessId, out var currentSession) || session != currentSession)
            throw new InvalidDataException("双开只支持当前 Windows 登录会话中的客户端。");
        var exactName = $@"\Sessions\{session}\BaseNamedObjects\{mutexName}";
        var released = 0;
        foreach (var handle in Snapshot(process))
        {
            if (!DuplicateHandle(process, handle, GetCurrentProcess(), out var copy, 0, false, 2)) continue;
            try
            {
                // Never query names on files/pipes, which can block NtQueryObject.
                if (QueryObject(copy, 2) != "Mutant" || QueryObject(copy, 1) != exactName) continue;
                // Recheck the live handle immediately before closing only this startup gate.
                if (!DuplicateHandle(process, handle, GetCurrentProcess(), out var check, 0, false, 2)) continue;
                try
                {
                    if (QueryObject(check, 2) != "Mutant" || QueryObject(check, 1) != exactName) continue;
                    if (!DuplicateHandle(process, handle, GetCurrentProcess(), out var removed, 0, false, 3))
                        throw new Win32Exception(Marshal.GetLastWin32Error()); // SAME_ACCESS | CLOSE_SOURCE
                    CloseHandle(removed);
                    released++;
                }
                finally { CloseHandle(check); }
            }
            finally { CloseHandle(copy); }
        }
        return released;
    }

    private static List<IntPtr> Snapshot(SafeProcessHandle process)
    {
        var size = 65536;
        while (size <= 16 * 1024 * 1024)
        {
            var buffer = Marshal.AllocHGlobal(size);
            try
            {
                var status = NtQueryInformationProcess(process, 51, buffer, size, out var needed);
                if (status == unchecked((int)0xc0000004)) { size = Math.Max(size * 2, needed + 4096); continue; }
                if (status != 0) throw new InvalidOperationException("无法检查客户端双开状态，请使用同一 Windows 用户启动登录器和游戏。");
                var count = Marshal.ReadIntPtr(buffer).ToInt64(); var stride = Marshal.SizeOf<HandleEntry>();
                if (count < 0 || count > (size - 2 * IntPtr.Size) / stride) throw new InvalidDataException("客户端句柄列表无效。");
                var result = new List<IntPtr>();
                for (var i = 0; i < count; i++) result.Add(Marshal.ReadIntPtr(buffer, 2 * IntPtr.Size + i * stride));
                return result;
            }
            finally { Marshal.FreeHGlobal(buffer); }
        }
        throw new InvalidDataException("客户端句柄列表过大，暂不执行双开操作。");
    }

    private static string QueryObject(IntPtr handle, int kind)
    {
        var buffer = Marshal.AllocHGlobal(65536);
        try
        {
            if (NtQueryObject(handle, kind, buffer, 65536, out _) != 0) return "";
            var length = (ushort)Marshal.ReadInt16(buffer);
            var pointer = Marshal.ReadIntPtr(buffer, IntPtr.Size == 8 ? 8 : 4);
            return pointer == IntPtr.Zero ? "" : Marshal.PtrToStringUni(pointer, length / 2) ?? "";
        }
        finally { Marshal.FreeHGlobal(buffer); }
    }

    [StructLayout(LayoutKind.Sequential)]
    private struct HandleEntry
    {
        public IntPtr Handle, HandleCount, PointerCount;
        public uint Access, Type, Attributes, Reserved;
    }
    [DllImport("kernel32", SetLastError = true)] private static extern SafeProcessHandle OpenProcess(uint access, bool inherit, int pid);
    [DllImport("kernel32", CharSet = CharSet.Unicode, SetLastError = true)] private static extern bool QueryFullProcessImageName(SafeProcessHandle process, uint flags, StringBuilder name, ref int size);
    [DllImport("kernel32", SetLastError = true)] private static extern bool GetProcessTimes(SafeProcessHandle process, out long start, out long exit, out long kernel, out long user);
    [DllImport("kernel32", SetLastError = true)] private static extern bool ProcessIdToSessionId(uint pid, out uint session);
    [DllImport("kernel32")] private static extern IntPtr GetCurrentProcess();
    [DllImport("kernel32", SetLastError = true)] private static extern bool DuplicateHandle(SafeProcessHandle source, IntPtr handle, IntPtr target, out IntPtr copy, uint access, bool inherit, uint options);
    [DllImport("kernel32")] private static extern bool CloseHandle(IntPtr handle);
    [DllImport("ntdll")] private static extern int NtQueryInformationProcess(SafeProcessHandle process, int kind, IntPtr buffer, int size, out int needed);
    [DllImport("ntdll")] private static extern int NtQueryObject(IntPtr handle, int kind, IntPtr buffer, int size, out int needed);
}
