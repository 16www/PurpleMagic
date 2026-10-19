using System.Diagnostics;
using System.Runtime.InteropServices;
using Microsoft.Win32.SafeHandles;
using Dnf90.Launcher;

static class BundleExitChecks
{
    [DllImport("kernel32.dll", SetLastError = true)]
    private static extern uint WaitForSingleObject(SafeProcessHandle process, uint milliseconds);

    public static async Task Run(string fixture)
    {
        var root = Path.Combine(Path.GetTempPath(), "dnf90-exit-tests-" + Guid.NewGuid().ToString("N"));
        Directory.CreateDirectory(root);
        try
        {
            foreach (var name in new[] { "DNF90.exe", "ExitDelay.dll" }) File.Copy(Path.Combine(fixture, name), Path.Combine(root, name));
            foreach (var scenario in new[] { "owner-wait", "unlisted-owner", "replacement-rollback", "wrong-start-time", "exclusive-check" })
            {
                File.Delete(Path.Combine(root, "detach.ready"));
                using var process = Process.Start(new ProcessStartInfo(Path.Combine(root, "DNF90.exe"))
                    { UseShellExecute = false, CreateNoWindow = true, WorkingDirectory = root })!;
                var handle = process.SafeHandle; var started = process.StartTime.ToUniversalTime().Ticks;
                try
                {
                    for (var i = 0; !File.Exists(Path.Combine(root, "detach.ready")) && i < 200; i++) await Task.Delay(10);
                    if (!File.Exists(Path.Combine(root, "detach.ready")) || !process.HasExited || process.ExitCode != 0 || WaitForSingleObject(handle, 0) != 258)
                        throw new Exception("Native fixture did not reproduce exit-code-before-kernel-signal window");
                    if (scenario == "owner-wait") await BundleProcesses.WaitForOwner(root, process.Id, started);
                    else if (scenario == "unlisted-owner") await BundleProcesses.StopInstalled(root);
                    else if (scenario == "replacement-rollback") await BundleProcesses.StopLaunched(process);
                    else if (scenario == "wrong-start-time")
                    {
                        await BundleProcesses.WaitForOwner(root, process.Id, started + 1);
                        if (WaitForSingleObject(handle, 0) != 258) throw new Exception("Mismatched process creation time was not ignored");
                        WaitForSingleObject(handle, 5000);
                    }
                    else
                    {
                        var rejected = false;
                        try { BundleProcesses.RequireExclusive(root); }
                        catch (IOException error) when ((error.HResult & 0xffff) == 32) { rejected = true; }
                        if (!rejected) throw new Exception("Exclusive check allowed a process still tearing down DLLs");
                        WaitForSingleObject(handle, 5000);
                    }
                    if (WaitForSingleObject(handle, 0) != 0)
                        throw new Exception(scenario + " returned before Windows completed DLL teardown");
                    BundleProcesses.RequireExclusive(root);
                    var moved = root + "-moved";
                    Directory.Move(root, moved); Directory.Move(moved, root);
                    Console.WriteLine("PASS native DLL teardown: " + scenario + "; exit-code 0 precedes kernel signal, directory released after wait");
                }
                finally
                {
                    // HasExited is deliberately insufficient in this test, too.
                    if (WaitForSingleObject(handle, 5000) != 0) { process.Kill(); WaitForSingleObject(handle, 5000); }
                }
            }
        }
        finally { Directory.Delete(root, true); }
    }
}
