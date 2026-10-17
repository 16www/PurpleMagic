using System.Diagnostics;
using System.Security.Cryptography;

namespace ServerS5A3;

// Exact derivative of the inspected paired EXE. No running-process modification,
// generic opcode bypass or acceptance of arbitrary client revisions.
public static class NativeGuildCompatibility
{
    public const string Original = "2AF985B6E344E5FDBB78F6598D40924F152AF924A156F5D45B671BED350A9675";
    public const string Patched = "E3FE061FD6918C11F2DDCA7359498BCD7BAF964C66600B2526FCEC6323A24329";
    public const string Pvf = "B86DD8FA5BCDE40A445D9E494A9E1A89D0FC15DDD15D61041E4B97F78973D00F";
    private const int Length = 111734784;
    private static readonly (int Offset, byte Before, byte After)[] Changes =
    [
        (111687008,229,253), (111687015,17,145), (111687017,132,188),
        (111687019,2,18), (111687043,0,88), (111687044,240,241),
        (111687085,0,224), (111687092,0,104), (111687093,0,3),
        (111687095,0,4), (111687181,0,52), (111687190,163,173)
    ];
    private static bool Equal(string a, string b) => a.Equals(b, StringComparison.OrdinalIgnoreCase);
    public static bool AcceptsDerivative(PlayerProfile profile, string actual) =>
        Equal(profile.ExeSha256, Original) && Equal(profile.PvfSha256, Pvf) && Equal(actual, Patched);

    internal static byte[] Transform(byte[] original)
    {
        if (original.Length != Length || !Equal(Convert.ToHexString(SHA256.HashData(original)), Original))
            throw new InvalidDataException("公会兼容补丁不适用于此客户端，原文件未修改。");
        var result = (byte[])original.Clone();
        foreach (var (offset, before, after) in Changes)
        {
            if (result[offset] != before) throw new InvalidDataException("公会兼容补丁基线不一致。");
            result[offset] = after;
        }
        if (!Equal(Convert.ToHexString(SHA256.HashData(result)), Patched)) throw new InvalidDataException("公会兼容补丁校验失败。");
        return result;
    }

    public static bool Prepare(string directory, PlayerProfile profile) => Prepare(directory, profile, () =>
    {
        var processes = Process.GetProcessesByName("DNF");
        try { return processes.Length != 0; }
        finally { foreach (var process in processes) process.Dispose(); }
    });

    // Call only after GameContract.VerifyClient (including the actual PVF).
    internal static bool Prepare(string directory, PlayerProfile profile, Func<bool> gameRunning)
    {
        if (!Equal(profile.ExeSha256, Original) || !Equal(profile.PvfSha256, Pvf)) return false;
        var target = Path.GetFullPath(Path.Combine(directory, "DNF.exe"));
        NoLinks(target);
        if (new FileInfo(target).Length != Length) throw new InvalidDataException("客户端长度不符合公会补丁基线。");
        // Deny concurrent writes, while allowing our atomic rename on Windows.
        using var source = new FileStream(target, FileMode.Open, FileAccess.Read, FileShare.Read | FileShare.Delete);
        var original = new byte[Length]; source.ReadExactly(original);
        var hash = Convert.ToHexString(SHA256.HashData(original));
        if (Equal(hash, Patched)) return false;
        if (!Equal(hash, Original)) throw new InvalidDataException("客户端摘要不符合公会补丁基线。");
        if (gameRunning()) throw new InvalidOperationException("应用公会兼容补丁前，请先退出游戏后重新登录。");
        var patched = Transform(original);
        var suffix = Guid.NewGuid().ToString("N");
        var stage = target + ".guild-" + suffix + ".tmp";
        var backup = target + ".guild-before-" + suffix + ".bak";
        try
        {
            using (var output = new FileStream(stage, FileMode.CreateNew, FileAccess.Write, FileShare.None))
            { output.Write(patched); output.Flush(true); }
            NoLinks(target);
            using (var current = File.OpenRead(target))
                if (!Equal(Convert.ToHexString(SHA256.HashData(current)), Original)) throw new IOException("客户端被其他程序修改，补丁未应用。");
            if (gameRunning()) throw new InvalidOperationException("游戏已启动，补丁未应用，请退出后重试。");
            File.Replace(stage, target, backup);
            using var saved = File.OpenRead(backup);
            if (!Equal(Convert.ToHexString(SHA256.HashData(saved)), Original))
            {
                saved.Dispose();
                File.Replace(backup, target, target + ".guild-rejected-" + suffix + ".bak");
                throw new IOException("检测到并发文件替换，已恢复替换前文件。");
            }
            using var installed = File.OpenRead(target);
            if (!Equal(Convert.ToHexString(SHA256.HashData(installed)), Patched)) throw new IOException("补丁安装后校验失败，原版备份已保留。");
            return true;
        }
        finally { if (File.Exists(stage)) File.Delete(stage); }
    }

    private static void NoLinks(string path)
    {
        for (var current = path; !string.IsNullOrEmpty(current); current = Path.GetDirectoryName(current))
            if ((File.Exists(current) || Directory.Exists(current)) && (File.GetAttributes(current) & FileAttributes.ReparsePoint) != 0)
                throw new IOException("客户端路径包含链接，未应用兼容补丁。");
    }
}
