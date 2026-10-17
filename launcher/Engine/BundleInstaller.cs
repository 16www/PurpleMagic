using System.Diagnostics;
using System.IO.Compression;
using System.Text.Json;
using ServerS5A3;

namespace Dnf90.Launcher;

public static class BundleInstaller
{
    public const string Marker = "DNF90-Flutter-bundle-v1";
    internal static string OutcomeFile(string root) => Path.Combine(root, ".launcher-update-result.json");
    internal static string? PreviousFailure(string root)
    {
        var file = OutcomeFile(root);
        SafeFiles.NoLinks(file);
        if (!File.Exists(file)) return null;
        var outcome = JsonSerializer.Deserialize(File.ReadAllBytes(file), WireJson.Default.BundleOutcome);
        if (outcome is null || outcome.Succeeded) return null;
        return outcome.Reason == "files-in-use"
            ? "上次登录器运行文件被占用或无法写入，更新未完成，已保留旧版。请关闭所有登录器窗口后重新打开，再安装登录器更新。"
            : "上次登录器更新未通过启动检查，已恢复旧版。请重试登录器更新，仍失败时下载新的完整便携入口。";
    }
    private static void Outcome(BundleJob job, string version, bool success, string reason) =>
        SafeFiles.Atomic(OutcomeFile(job.InstallRoot), JsonSerializer.SerializeToUtf8Bytes(new BundleOutcome(version, success, reason), WireJson.Default.BundleOutcome));
    public static void ValidateRoot(string root)
    {
        SafeFiles.NoLinks(root);
        SafeFiles.NoLinks(Path.Combine(root, "launcher.id"));
        if (!Path.IsPathFullyQualified(root) || !File.Exists(Path.Combine(root, "launcher.id")) || File.ReadAllText(Path.Combine(root, "launcher.id")).Trim() != Marker)
            throw new InvalidDataException("当前不是支持整体更新的登录器安装目录。");
    }
    public static void Extract(string package, string destination)
    {
        SafeFiles.NoLinks(destination); Directory.CreateDirectory(destination);
        using var zip = ZipFile.OpenRead(package);
        if (zip.Entries.Count is < 5 or > 2000) throw new InvalidDataException("登录器更新包文件数无效。");
        var names = new HashSet<string>(StringComparer.OrdinalIgnoreCase); long total = 0;
        foreach (var entry in zip.Entries)
        {
            if (entry.FullName.EndsWith('/')) continue;
            var path = SafeFiles.Target(destination, entry.FullName);
            if (!names.Add(entry.FullName) || (entry.ExternalAttributes >> 16 & 0xf000) == 0xa000 || (entry.ExternalAttributes & 0x400) != 0 || entry.Length < 0 || (total += entry.Length) > 500L * 1024 * 1024)
                throw new InvalidDataException("登录器包包含重复路径、链接或超大文件。");
            Directory.CreateDirectory(Path.GetDirectoryName(path)!); entry.ExtractToFile(path, false);
        }
        foreach (var name in new[] { "DNF90.exe", "DNF90.Core.exe", "flutter_windows.dll", "data/app.so", "data/icudtl.dat" })
            if (!names.Contains(name)) throw new InvalidDataException("登录器包缺少必要文件。");
        foreach (var name in new[] { "DNF90.exe", "DNF90.Core.exe", "flutter_windows.dll" })
        { using var stream = File.OpenRead(Path.Combine(destination, name)); if (stream.ReadByte() != 'M' || stream.ReadByte() != 'Z') throw new InvalidDataException("更新包程序格式无效。"); }
    }
    private static void MoveDirectory(string from, string to)
    {
        var timer = Stopwatch.StartNew();
        while (true)
        {
            SafeFiles.NoLinks(from); SafeFiles.NoLinks(to);
            try { Directory.Move(from, to); return; }
            catch (IOException) when (timer.Elapsed < TimeSpan.FromSeconds(20)) { Thread.Sleep(100); }
            catch (UnauthorizedAccessException) when (timer.Elapsed < TimeSpan.FromSeconds(20)) { Thread.Sleep(100); }
        }
    }
    private static void Save(BundleJob job) => SafeFiles.Atomic(Path.Combine(job.InstallRoot, ".launcher-update.json"), JsonSerializer.SerializeToUtf8Bytes(job, WireJson.Default.BundleJob));
    private static void ValidateJob(BundleJob job)
    {
        ValidateRoot(job.InstallRoot);
        var parent = Path.Combine(job.InstallRoot, ".updates") + Path.DirectorySeparatorChar;
        if (!Path.GetFullPath(job.Work).StartsWith(parent, StringComparison.OrdinalIgnoreCase) || Path.GetDirectoryName(job.Work) != parent.TrimEnd(Path.DirectorySeparatorChar)) throw new InvalidDataException("更新事务目录无效。");
        SafeFiles.NoLinks(job.Work);
    }
    public static void Recover(string root)
    {
        ValidateRoot(root); var journal = Path.Combine(root, ".launcher-update.json"); if (!File.Exists(journal)) return;
        var job = JsonSerializer.Deserialize(File.ReadAllBytes(journal), WireJson.Default.BundleJob) ?? throw new InvalidDataException(); ValidateJob(job);
        var app = Path.Combine(root, "app"); var backup = Path.Combine(job.Work, "previous");
        if (job.Phase != "healthy" && Directory.Exists(backup))
        {
            if (Directory.Exists(app)) MoveDirectory(app, Path.Combine(job.Work, "failed-" + Guid.NewGuid().ToString("N")));
            MoveDirectory(backup, app);
        }
        File.Delete(journal); // Keep prior/candidate artifacts; never delete user files recursively.
    }
    public static async Task<int> Apply(BundleJob job, PlayerProfile root)
    {
        ValidateJob(job);
        var raw = Convert.FromBase64String(job.Envelope); var release = Trust.Verify(raw, root).Launcher ?? throw new InvalidDataException("缺少登录器发布信息。");
        if (!await SafeFiles.Matches(job.Package, release.Size, release.Sha256)) throw new InvalidDataException("登录器包摘要不符。");
        await WaitFor(job.UiPid, job.UiStart); await WaitFor(job.CorePid, job.CoreStart);
        using var gate = new FileStream(Path.Combine(job.InstallRoot, ".launcher-install.lock"), FileMode.OpenOrCreate, FileAccess.ReadWrite, FileShare.None);
        if (File.Exists(Path.Combine(job.InstallRoot, ".launcher-update.json"))) Recover(job.InstallRoot);
        Process? launched = null;
        try
        {
            BundleProcesses.RequireExclusive(Path.Combine(job.InstallRoot, "app"));
            var next = Path.Combine(job.Work, "next"); Extract(job.Package, next);
            job = job with { Phase = "prepared" }; Save(job);
            MoveDirectory(Path.Combine(job.InstallRoot, "app"), Path.Combine(job.Work, "previous")); job = job with { Phase = "old-moved" }; Save(job);
            MoveDirectory(next, Path.Combine(job.InstallRoot, "app")); job = job with { Phase = "candidate-installed" }; Save(job);
            SafeFiles.NoLinks(OutcomeFile(job.InstallRoot)); File.Delete(OutcomeFile(job.InstallRoot));
            var health = Path.Combine(job.Work, "ready");
            var start = new ProcessStartInfo(Path.Combine(job.InstallRoot, "app", "DNF90.exe")) { UseShellExecute = false, WorkingDirectory = Path.Combine(job.InstallRoot, "app") };
            start.ArgumentList.Add("--update-health"); start.ArgumentList.Add(health);
            launched = Process.Start(start);
            for (var i = 0; i < 600; i++)
            {
                if (File.Exists(health)) { Save(job with { Phase = "healthy" }); Outcome(job, release.Version, true, ""); File.Delete(Path.Combine(job.InstallRoot, ".launcher-update.json")); return 0; }
                if (launched is null || launched.HasExited) break;
                await Task.Delay(100);
            }
            throw new InvalidOperationException("新登录器未返回就绪回执。");
        }
        catch (Exception error)
        {
            // Only this replacement UI may be terminated. No game processes are touched.
            if (launched is not null && !launched.HasExited) { launched.Kill(); await launched.WaitForExitAsync(); }
            Recover(job.InstallRoot);
            Outcome(job, release.Version, false, error is IOException or UnauthorizedAccessException ? "files-in-use" : "startup-failed");
            Process.Start(new ProcessStartInfo(Path.Combine(job.InstallRoot, "app", "DNF90.exe")) { UseShellExecute = false, WorkingDirectory = Path.Combine(job.InstallRoot, "app") });
            return 5;
        }
        finally { launched?.Dispose(); }
    }
    private static async Task WaitFor(int pid, long started)
    {
        try { using var process = Process.GetProcessById(pid); if (process.StartTime.ToUniversalTime().Ticks != started) return; using var timeout = new CancellationTokenSource(TimeSpan.FromMinutes(2)); await process.WaitForExitAsync(timeout.Token); }
        catch (ArgumentException) { }
    }
}
