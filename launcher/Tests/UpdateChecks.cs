using System.Diagnostics;
using System.IO.Compression;
using System.Reflection;
using System.Security.Cryptography;
using ServerS5A3;

internal static class UpdateChecks
{
    internal static void InstanceFixture(string name, string mode)
    {
        if (mode == "legacy")
        {
            using var legacy = new Mutex(true, name, out var createdNew);
            Console.WriteLine(createdNew ? "ACQUIRED" : "BLOCKED");
            if (createdNew) legacy.ReleaseMutex();
            return;
        }
        using var mutex = new Mutex(false, name);
        var acquired = LauncherInstance.TryAcquire(mutex);
        Console.WriteLine(acquired ? "ACQUIRED" : "BLOCKED");
        Console.Out.Flush();
        if (acquired && mode != "abandon") mutex.ReleaseMutex();
    }

    private static string Child(string name, string mode)
    {
        var start = new ProcessStartInfo(Environment.ProcessPath!) {
            UseShellExecute = false, CreateNoWindow = true, RedirectStandardOutput = true, RedirectStandardError = true
        };
        if (Path.GetFileNameWithoutExtension(start.FileName).Equals("dotnet", StringComparison.OrdinalIgnoreCase))
            start.ArgumentList.Add(Assembly.GetExecutingAssembly().Location);
        foreach (var arg in new[] { "--update-instance-fixture", name, mode }) start.ArgumentList.Add(arg);
        using var process = Process.Start(start)!;
        try
        {
            if (!process.WaitForExit(10000)) throw new Exception("Update fixture timeout");
            var result = process.StandardOutput.ReadToEnd().Trim();
            if (process.ExitCode != 0) throw new Exception("Update fixture failed: " + process.StandardError.ReadToEnd());
            return result;
        }
        finally { if (!process.HasExited) { process.Kill(); process.WaitForExit(); } }
    }

    internal static void Run(Action<bool, string> check, Action<Action, string> reject)
    {
        // Reproduce the old helper's lifetime: its released mutex HANDLE remains
        // open while a fresh process tries to start the replacement launcher.
        var name = "ServerS5A3-update-test-" + Guid.NewGuid().ToString("N");
        using var helper = new Mutex(false, name);
        check(LauncherInstance.TryAcquire(helper), "fixture helper owns instance during replacement");
        helper.ReleaseMutex();
        check(Child(name, "legacy") == "BLOCKED", "original startup falsely rejects a released helper mutex");
        check(Child(name, "fixed") == "ACQUIRED", "replacement starts while old helper keeps the released handle");
        check(LauncherInstance.TryAcquire(helper), "helper can reacquire after replacement exits");
        try { check(Child(name, "fixed") == "BLOCKED", "a live owning launcher still excludes a second instance"); }
        finally { helper.ReleaseMutex(); }
        check(Child(name, "abandon") == "ACQUIRED", "fixture creates an abandoned instance lock");
        check(LauncherInstance.TryAcquire(helper), "abandoned launcher ownership can be recovered");
        helper.ReleaseMutex();
        helper.Dispose();
        check(Child(name, "legacy") == "ACQUIRED", "disposing helper handle also permits an old launcher rollback to restart");

        if (!OperatingSystem.IsWindows()) return;
        var directory = Path.Combine(Path.GetTempPath(), "servers5a3-update-lock-" + Guid.NewGuid().ToString("N"));
        Directory.CreateDirectory(directory);
        try
        {
            var exe = Path.Combine(directory, "ServerS5A3.exe");
            File.WriteAllText(exe, "MZold");
            var package = Path.Combine(directory, "new.zip");
            using (var zip = ZipFile.Open(package, ZipArchiveMode.Create))
            using (var writer = new StreamWriter(zip.CreateEntry("ServerS5A3.exe").Open())) writer.Write("MZnew");
            var release = new ReleaseManifest("0.1.6", "https://example.invalid/package.zip", new FileInfo(package).Length,
                Convert.ToHexString(SHA256.HashData(File.ReadAllBytes(package))));
            void WhileTemporarilyLocked(Action action)
            {
                using var locked = new FileStream(exe, FileMode.Open, FileAccess.Read, FileShare.None);
                var releaseLock = Task.Run(async () => { await Task.Delay(350); locked.Dispose(); });
                try { action(); } finally { releaseLock.GetAwaiter().GetResult(); }
            }
            WhileTemporarilyLocked(() => SignedUpdate.Apply(package, release, exe, TimeSpan.FromSeconds(3)));
            check(File.ReadAllText(exe) == "MZnew" && File.ReadAllText(exe + ".bak") == "MZold", "temporary Windows sharing violation retries to a verified replacement");
            WhileTemporarilyLocked(() => SignedUpdate.Rollback(exe, TimeSpan.FromSeconds(3)));
            check(File.ReadAllText(exe) == "MZold" && File.ReadAllText(exe + ".failed") == "MZnew", "temporary rollback lock preserves and restores correct files");
            File.WriteAllText(exe + ".bak", "previous-backup");
            using (var locked = new FileStream(exe, FileMode.Open, FileAccess.Read, FileShare.None))
                reject(() => SignedUpdate.Apply(package, release, exe, TimeSpan.FromMilliseconds(200)), "persistent lock stops after bounded retry");
            check(File.ReadAllText(exe) == "MZold" && File.ReadAllText(exe + ".bak") == "previous-backup", "persistent lock leaves current launcher and previous backup unchanged");
            check(Directory.GetFiles(directory, "*.new").Length == 0, "failed replacement cleans its unique staging file");
            reject(() => SignedUpdate.Apply(package, release with { Sha256 = new string('0', 64) }, exe), "file-lock recovery never bypasses package verification");
        }
        finally { Directory.Delete(directory, true); }
    }
}
