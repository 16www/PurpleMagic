using System.IO.Compression;
using System.Diagnostics;
using System.Runtime.InteropServices;
using System.Security.Cryptography;
using System.Text.Json;
using Dnf90.Launcher;
using Microsoft.Win32.SafeHandles;
using ServerS5A3;

static class BundleLifecycleChecks
{
    [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
    private static extern SafeFileHandle CreateFile(string path, uint access, uint share, IntPtr security, uint disposition, uint flags, IntPtr template);

    public static async Task Run(string fixture)
    {
        var root = Path.Combine(Path.GetTempPath(), "dnf90-bundle-lifecycle-" + Guid.NewGuid().ToString("N"));
        Directory.CreateDirectory(root);
        using var key = RSA.Create(2048);
        var trusted = new PlayerProfile("https://accounts.example.test", new string('a', 64), key.ExportSubjectPublicKeyInfoPem(), "https://accounts.example.test/settings", "192.0.2.1", 7001, new string('b', 64), new string('c', 64), "");
        try
        {
            var files = Directory.GetFiles(fixture).ToDictionary(p => Path.GetFileName(p), File.ReadAllBytes);
            files["DNF90.exe"] = File.ReadAllBytes(Path.Combine(fixture, "Launcher.UpdateFixture.exe"));
            files["DNF90.Core.exe"] = files["DNF90.exe"];
            files["flutter_windows.dll"] = "MZfixture"u8.ToArray();
            files["data/app.so"] = "new application"u8.ToArray();
            files["data/icudtl.dat"] = "fixture data"u8.ToArray();
            foreach (var scenario in new[] { "success", "wait-for-owners", "locked-directory", "unhealthy" })
            {
                var success = scenario is "success" or "wait-for-owners";
                var install = Path.Combine(root, scenario); var app = Path.Combine(install, "app");
                Directory.CreateDirectory(app); File.WriteAllText(Path.Combine(install, "launcher.id"), BundleInstaller.Marker);
                foreach (var (name, raw) in files) { var path = Path.Combine(app, name); Directory.CreateDirectory(Path.GetDirectoryName(path)!); File.WriteAllBytes(path, raw); }
                File.WriteAllText(Path.Combine(app, "data/app.so"), "old application");
                File.WriteAllText(Path.Combine(install, "unmanaged-settings"), "preserve");
                if (scenario == "success")
                {
                    var marker = Path.Combine(install, "fixture-running");
                    var start = new ProcessStartInfo(Path.Combine(app, "DNF90.exe")) { UseShellExecute = false, CreateNoWindow = true };
                    start.ArgumentList.Add("--hold-ready"); start.ArgumentList.Add(marker);
                    using var peer = Process.Start(start) ?? throw new Exception("Fixture did not start");
                    try
                    {
                        for (var i = 0; !File.Exists(marker) && i < 100; i++) await Task.Delay(50);
                        if (!File.Exists(marker)) throw new Exception("Fixture did not signal readiness");
                        var refused = false;
                        try { BundleProcesses.RequireExclusive(app); } catch (IOException) { refused = true; }
                        if (!refused) throw new Exception("Another installed UI was ignored");
                        BundleProcesses.RequireExclusive(app, peer.Id);
                        Console.WriteLine("PASS duplicate UI preflight and expected owner exception");
                    }
                    finally { if (!peer.HasExited) peer.Kill(); await peer.WaitForExitAsync(); }
                }
                var work = Path.Combine(install, ".updates", Guid.NewGuid().ToString("N")); Directory.CreateDirectory(work);
                var package = Path.Combine(work, "bundle.zip");
                using (var zip = ZipFile.Open(package, ZipArchiveMode.Create))
                {
                    foreach (var (name, raw) in files) { using var output = zip.CreateEntry(name).Open(); output.Write(raw); }
                    if (scenario == "unhealthy") { using var output = zip.CreateEntry("unhealthy").Open(); output.WriteByte(1); }
                }
                var bytes = File.ReadAllBytes(package);
                var release = new BundleRelease("0.2.99", "https://accounts.example.test/bundle.zip", bytes.Length, Trust.Digest(bytes));
                var payload = JsonSerializer.SerializeToUtf8Bytes(new Bootstrap("DNF90-Flutter", 1, 1, trusted, [], null, release), WireJson.Default.Bootstrap);
                var envelope = JsonSerializer.SerializeToUtf8Bytes(new SignedEnvelope(Convert.ToBase64String(payload), Convert.ToBase64String(key.SignData(payload, HashAlgorithmName.SHA256, RSASignaturePadding.Pss))), CoreJson.Default.SignedEnvelope);
                var job = new BundleJob(install, package, Convert.ToBase64String(envelope), -1, 0, -1, 0, work, "downloaded");
                using var locked = scenario == "locked-directory" ? CreateFile(app, 0x80000000, 3, IntPtr.Zero, 3, 0x02000000, IntPtr.Zero) : null;
                if (locked is { IsInvalid: true }) throw new Exception("Cannot establish directory-in-use regression");
                int code;
                if (scenario == "wait-for-owners")
                {
                    Process Start(string file)
                    {
                        var start = new ProcessStartInfo(Path.Combine(app, file)) { UseShellExecute = false, CreateNoWindow = true };
                        start.ArgumentList.Add("--hold-ready"); start.ArgumentList.Add(Path.Combine(install, file + ".started"));
                        return Process.Start(start) ?? throw new Exception("owner fixture failed");
                    }
                    using var ui = Start("DNF90.exe"); using var core = Start("DNF90.Core.exe");
                    try
                    {
                        job = job with { UiPid = ui.Id, UiStart = ui.StartTime.ToUniversalTime().Ticks, CorePid = core.Id, CoreStart = core.StartTime.ToUniversalTime().Ticks };
                        var applying = BundleInstaller.Apply(job, trusted);
                        await Task.Delay(250);
                        if (applying.IsCompleted || Directory.Exists(Path.Combine(work, "next"))) throw new Exception("Replaced files before UI/core exit");
                        ui.Kill(); await ui.WaitForExitAsync();
                        await Task.Delay(150);
                        if (applying.IsCompleted) throw new Exception("Ignored live core owner");
                        core.Kill(); await core.WaitForExitAsync();
                        code = await applying;
                    }
                    finally { if (!ui.HasExited) ui.Kill(); if (!core.HasExited) core.Kill(); }
                }
                else code = await BundleInstaller.Apply(job, trusted);
                if (code != (success ? 0 : 5)) throw new Exception("Unexpected updater result " + scenario + ": " + code);
                var expected = success ? "new application" : "old application";
                if (File.ReadAllText(Path.Combine(app, "data/app.so")) != expected || File.ReadAllText(Path.Combine(install, "unmanaged-settings")) != "preserve") throw new Exception("Installation or settings mismatch " + scenario);
                var outcome = JsonSerializer.Deserialize(File.ReadAllBytes(BundleInstaller.OutcomeFile(install)), WireJson.Default.BundleOutcome)!;
                if (outcome.Succeeded != success || outcome.Version != "0.2.99") throw new Exception("Missing durable upgrade result");
                if (!success && BundleInstaller.PreviousFailure(install) is null) throw new Exception("Rollback remains invisible");
                if (scenario == "locked-directory" && (!Directory.Exists(Path.Combine(work, "next")) || Directory.Exists(Path.Combine(work, "previous")))) throw new Exception("Did not reproduce player's pre-rename failure");
                Console.WriteLine("PASS real helper lifecycle: " + scenario);
                await Task.Delay(350); // Let the owned headless fallback fixture terminate.
            }
        }
        finally { Directory.Delete(root, true); }
    }
}
