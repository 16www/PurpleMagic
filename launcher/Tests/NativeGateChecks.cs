using System.Diagnostics;
using System.Reflection;
using System.Runtime.InteropServices;
using ServerS5A3;

internal static class NativeGateChecks
{
    [DllImport("kernel32", CharSet = CharSet.Unicode, SetLastError = true)]
    private static extern IntPtr CreateMutex(IntPtr attributes, bool owner, string name);
    [DllImport("kernel32")] private static extern bool CloseHandle(IntPtr handle);

    internal static void Fixture(string name)
    {
        // Test-owned process and unique names only; no DNF process is inspected or modified.
        var gate = CreateMutex(IntPtr.Zero, true, name);
        var unrelated = CreateMutex(IntPtr.Zero, false, name + "-unrelated");
        if (gate == IntPtr.Zero || unrelated == IntPtr.Zero) throw new Exception("fixture mutex creation failed");
        Console.WriteLine("READY"); Console.Out.Flush(); Console.ReadLine();
        CloseHandle(unrelated);
        // The compatibility check intentionally closes gate in this fixture. Process exit cleans any remaining handle.
    }

    internal static void Run(Action<bool, string> check, Action<Action, string> reject)
    {
        var name = "ServerS5A3-test-gate-" + Guid.NewGuid().ToString("N");
        var start = new ProcessStartInfo(Environment.ProcessPath!) { UseShellExecute = false, CreateNoWindow = true, RedirectStandardInput = true, RedirectStandardOutput = true };
        if (Path.GetFileNameWithoutExtension(start.FileName).Equals("dotnet", StringComparison.OrdinalIgnoreCase))
            start.ArgumentList.Add(Assembly.GetExecutingAssembly().Location);
        start.ArgumentList.Add("--native-gate-fixture"); start.ArgumentList.Add(name);
        using var child = Process.Start(start)!;
        try
        {
            var ready = child.StandardOutput.ReadLineAsync();
            if (!ready.Wait(TimeSpan.FromSeconds(10)) || ready.Result != "READY") throw new Exception("fixture failed to start");
            var exe = child.MainModule!.FileName; var created = child.StartTime.ToUniversalTime().ToFileTimeUtc();
            bool Exists(string n) { if (!Mutex.TryOpenExisting(n, out var mutex)) return false; mutex.Dispose(); return true; }
            check(Exists(name), "fixture gate initially blocks second instance");
            reject(() => NativeClientGate.ReleaseExactMutex(child.Id, created + 1, exe, name), "reject reused process identity");
            reject(() => NativeClientGate.ReleaseExactMutex(child.Id, created, exe + ".wrong", name), "reject foreign executable");
            check(NativeClientGate.ReleaseExactMutex(child.Id, created, exe, name + "-missing") == 0 && Exists(name), "exact name only");
            check(NativeClientGate.ReleaseExactMutex(child.Id, created, exe, name) == 1, "release only identified startup gate");
            check(!Exists(name) && Exists(name + "-unrelated") && !child.HasExited, "gate removed while process and unrelated mutex survive");
            using var second = new Mutex(false, name, out var createdNew);
            check(createdNew, "second instance can create its own gate");
            check(NativeClientGate.ReleaseExactMutex(child.Id, created, exe, name) == 0 && Exists(name), "never closes another process gate");
        }
        finally
        {
            if (!child.HasExited) { child.StandardInput.WriteLine("quit"); child.StandardInput.Flush(); }
            if (!child.WaitForExit(5000)) { child.Kill(); child.WaitForExit(); }
        }
    }
}
