using System.Buffers.Binary;
using System.Diagnostics;
using System.Runtime.InteropServices;
using System.Text;

namespace ServerS5A3;

// A build-supplied, version-specific INI section for the fixed native compatibility
// identity in GameContract. Never import a player's complete configuration.
public sealed record ChannelHistoryCompatibility(int WindowsCodePage, string AccountSectionBase64);

public static class NativeChannelHistory
{
    // Both inspected executables retain byte-identical argument parsing, account
    // section derivation and remembered-channel selection. Keep an exact allowlist.
    private static readonly HashSet<string> SupportedExecutables = new(StringComparer.OrdinalIgnoreCase)
    {
        "BC613E368E8FD49C50C35163F2ED8A28564FC23342701A5A6C22B839F3BF6B7A",
        "2AF985B6E344E5FDBB78F6598D40924F152AF924A156F5D45B671BED350A9675"
    };
    private const string SupportedPvf = "B86DD8FA5BCDE40A445D9E494A9E1A89D0FC15DDD15D61041E4B97F78973D00F";
    private const string Key = "CONFIG_CHARACTER_LATELY_GAMESERVER_INFO";
    private const int Limit = 256 * 1024;

    public static bool Prepare(PlayerProfile profile, ChannelProbeResult directory)
    {
        if (!OperatingSystem.IsWindows()) throw new PlatformNotSupportedException();
        if (profile.ChannelHistory is not { } compatibility) return false;
        ValidateCompatibility(profile, GetACP());
        ChannelProbe.ValidateDirectoryServer(directory.ServerId, profile.ChannelServerId, profile.PreviousChannelServerId);
        return PrepareFile(ConfigurationPath(), compatibility, directory.ChannelIds, () => {
            var running = Process.GetProcessesByName("DNF");
            try { return running.Length != 0; }
            finally { foreach (var process in running) process.Dispose(); }
        }, directory.ServerId);
    }

    public static string ConfigurationPath()
    {
        if (!OperatingSystem.IsWindows()) throw new PlatformNotSupportedException();
        var folder = new Guid("A520A1A4-1780-4FF6-BD18-167343C5AF16"); // FOLDERID_LocalAppDataLow
        var result = SHGetKnownFolderPath(ref folder, 0, IntPtr.Zero, out var pointer);
        if (result != 0) Marshal.ThrowExceptionForHR(result);
        string path;
        try { path = Path.Combine(Marshal.PtrToStringUni(pointer)!, "DNF", "DNF.cfg"); }
        finally { Marshal.FreeCoTaskMem(pointer); }
        return path;
    }

    internal static void ValidateCompatibility(PlayerProfile profile, uint windowsCodePage)
    {
        if (profile.ChannelHistory is not { } compatibility ||
            !SupportedExecutables.Contains(profile.ExeSha256) ||
            !profile.PvfSha256.Equals(SupportedPvf, StringComparison.OrdinalIgnoreCase) ||
            compatibility.WindowsCodePage != 936 || windowsCodePage != compatibility.WindowsCodePage)
            throw new InvalidOperationException("频道初始化 C05：客户端版本或 Windows 语言环境尚未支持，请反馈此检测码。");
    }

    internal static bool PrepareFile(string path, ChannelHistoryCompatibility compatibility, int[] available, Func<bool> gameRunning, int advertisedServer = 0)
    {
        var existed = File.Exists(path);
        if (existed && new FileInfo(path).Length > Limit) throw new InvalidDataException("频道配置超过 256 KB，原文件未修改。请在新版登录器设置中备份并重置本机游戏配置。");
        var original = existed ? File.ReadAllBytes(path) : Array.Empty<byte>();
        var updated = Transform(original, compatibility, available, advertisedServer);
        if (updated.AsSpan().SequenceEqual(original)) return false;
        if (gameRunning()) throw new InvalidOperationException("需要初始化频道，请先退出游戏，再重新登录。");
        Directory.CreateDirectory(Path.GetDirectoryName(path)!);
        var temporary = path + ".ServerS5A3-" + Guid.NewGuid().ToString("N") + ".tmp";
        try
        {
            File.WriteAllBytes(temporary, updated);
            // Verify with the same Windows profile-struct API used by the native
            // client, including its checksum. This never starts a game process.
            if (!TryReadNative(temporary, compatibility, out var server, out var channel) || server != advertisedServer || channel != 10)
                throw new InvalidDataException("频道初始化校验失败，未修改原配置。");
            if (gameRunning()) throw new InvalidOperationException("游戏正在运行，原配置未修改；请退出后重试。");
            if (File.Exists(path) != existed || (existed && !File.ReadAllBytes(path).AsSpan().SequenceEqual(original)))
                throw new IOException("游戏配置已被其他程序更新，请重试。");
            if (existed)
            {
                var backup = path + ".ServerS5A3-" + DateTime.UtcNow.ToString("yyyyMMddHHmmss") + "-" + Guid.NewGuid().ToString("N") + ".bak";
                File.Replace(temporary, path, backup);
            }
            else File.Move(temporary, path); // Refuse to overwrite a concurrently created file.
            return true;
        }
        finally { if (File.Exists(temporary)) File.Delete(temporary); }
    }

    internal static byte[] Transform(byte[] original, ChannelHistoryCompatibility compatibility, int[] available, int advertisedServer = 0)
    {
        if (advertisedServer is < 0 or >= 250) throw new InvalidDataException("频道初始化区服编号无效。");
        if (!available.Contains(10)) throw new InvalidDataException("频道初始化 C06：服务器未提供已验证的登录频道。");
        var section = Section(compatibility);
        if (original.Length > Limit || original.Contains((byte)0) || original.AsSpan().StartsWith(new byte[] { 0xef, 0xbb, 0xbf }))
            throw new InvalidDataException("频道初始化 C05：配置编码尚未支持，原配置未修改。");
        // Latin1 is a byte-preserving view, not the file's text encoding.
        var text = Encoding.Latin1.GetString(original);
        var expected = Encoding.Latin1.GetString(section);
        var active = false; var sections = 0; var headerEnd = -1;
        var valueStart = -1; var valueEnd = -1;
        for (var offset = 0; offset < text.Length;)
        {
            var end = text.IndexOf('\n', offset); if (end < 0) end = text.Length;
            var line = text[offset..end].TrimEnd('\r');
            if (line.StartsWith('[') && line.EndsWith(']'))
            {
                active = line[1..^1] == expected;
                if (active && ++sections > 1) throw new InvalidDataException("频道配置包含重复区域，未修改原文件。");
                if (active) headerEnd = offset + line.Length;
            }
            else if (active && line.StartsWith(Key + "=", StringComparison.OrdinalIgnoreCase))
            {
                if (valueStart >= 0) throw new InvalidDataException("频道配置包含重复项目，未修改原文件。");
                valueStart = offset + Key.Length + 1; valueEnd = offset + line.Length;
            }
            offset = end + 1;
        }
        if (valueStart >= 0 && ValidHistory(text[valueStart..valueEnd], available, advertisedServer)) return original;
        // Two LE int32s (verified directory server, real channel 10), followed by the
        // byte-sum checksum required by GetPrivateProfileStruct.
        var identity = new byte[9];
        BinaryPrimitives.WriteInt32LittleEndian(identity, advertisedServer);
        BinaryPrimitives.WriteInt32LittleEndian(identity.AsSpan(4), 10);
        identity[8] = unchecked((byte)identity.Take(8).Sum(value => value));
        var landing = Convert.ToHexString(identity);
        if (valueStart >= 0) text = text[..valueStart] + landing + text[valueEnd..];
        else if (sections == 0)
            text += (text.Length != 0 && !text.EndsWith('\n') ? "\r\n" : "") + "[" + expected + "]\r\n" + Key + "=" + landing + "\r\n";
        else
        {
            var start = headerEnd;
            text = text[..start] + "\r\n" + Key + "=" + landing + text[start..];
        }
        return Encoding.Latin1.GetBytes(text);
    }

    private static bool ValidHistory(string text, int[] available, int advertisedServer)
    {
        if (text.Length != 18) return false;
        byte[] data;
        try { data = Convert.FromHexString(text); } catch (FormatException) { return false; }
        return data[8] == (byte)data.Take(8).Sum(value => value) &&
            BinaryPrimitives.ReadInt32LittleEndian(data) == advertisedServer &&
            available.Contains(BinaryPrimitives.ReadInt32LittleEndian(data.AsSpan(4)));
    }

    private static byte[] Section(ChannelHistoryCompatibility compatibility)
    {
        var section = Convert.FromBase64String(compatibility.AccountSectionBase64);
        if (compatibility.WindowsCodePage != 936 || section.Length is < 1 or > 128 ||
            section.Any(value => value is 0 or 10 or 13 or (byte)'[' or (byte)']'))
            throw new InvalidDataException("频道初始化配置无效。");
        return section;
    }

    internal static bool TryReadNative(string path, ChannelHistoryCompatibility compatibility, out int server, out int channel)
    {
        Encoding.RegisterProvider(CodePagesEncodingProvider.Instance);
        var section = Encoding.GetEncoding(compatibility.WindowsCodePage).GetString(Section(compatibility));
        var data = new byte[8];
        var read = GetPrivateProfileStruct(section, Key, data, (uint)data.Length, path);
        server = BinaryPrimitives.ReadInt32LittleEndian(data);
        channel = BinaryPrimitives.ReadInt32LittleEndian(data.AsSpan(4));
        return read;
    }

    [DllImport("kernel32.dll")] private static extern uint GetACP();
    [DllImport("shell32.dll")] private static extern int SHGetKnownFolderPath(ref Guid id, uint flags, IntPtr token, out IntPtr path);
    [DllImport("kernel32.dll", EntryPoint = "GetPrivateProfileStructW", CharSet = CharSet.Unicode)]
    [return: MarshalAs(UnmanagedType.Bool)]
    private static extern bool GetPrivateProfileStruct(string section, string key, [Out] byte[] data, uint size, string path);
}
