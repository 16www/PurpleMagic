using System.ComponentModel;
using System.Text.Json;
using Dnf90.Launcher;

internal static class BundleFailureChecks
{
    internal static void Run(string temp, Action<bool,string> check)
    {
        foreach (var (code, reason) in new[] { (32,"sharing-violation"),(33,"sharing-violation"),(5,"access-denied"),(112,"disk-full"),(39,"disk-full"),(183,"io-error") })
        {
            var error = new IOException("private path must not be persisted", unchecked((int)0x80070000) | code);
            check(BundleInstaller.FailureReason(error)==reason, "distinct Windows update failure " + code);
        }
        check(BundleInstaller.FailureReason(new Win32Exception(5))=="access-denied" && BundleInstaller.NativeError(new Win32Exception(5))==5,"native startup error uses actual Windows code");
        check(BundleInstaller.FailureReason(new InvalidOperationException())=="startup-failed","health failure remains separate from file locks");
        foreach(var reason in new[]{"sharing-violation","access-denied","disk-full","io-error","files-in-use"})
            check(!BundleInstaller.SuppressAutomaticRetry(new("0.2.12",false,reason),"0.2.12"),"fresh launch can retry corrected file failure " + reason);
        check(BundleInstaller.SuppressAutomaticRetry(new("0.2.12",false,"startup-failed"),"0.2.12"),"same unhealthy app does not create an automatic rollback loop");
        check(!BundleInstaller.SuppressAutomaticRetry(new("0.2.12",false,"startup-failed"),"0.2.13"),"new release is not blocked by old health failure");
        var root=Path.Combine(temp,"failure-outcomes");Directory.CreateDirectory(root);
        File.WriteAllText(BundleInstaller.OutcomeFile(root),"{\"version\":\"0.2.10\",\"succeeded\":false,\"reason\":\"files-in-use\"}");
        check(BundleInstaller.PreviousFailure(root)!.Contains("未记录具体原因"),"legacy ambiguous outcome does not assert a current file lock");
        var detailed=new BundleOutcome("0.2.12",false,"access-denied"){Stage="move-old-app",NativeError=5,FailedAtUtc=DateTimeOffset.UtcNow.ToString("O")};
        File.WriteAllBytes(BundleInstaller.OutcomeFile(root),JsonSerializer.SerializeToUtf8Bytes(detailed,WireJson.Default.BundleOutcome));
        check(BundleInstaller.ReadOutcome(root)==detailed,"AOT outcome metadata preserves failure evidence");
        check(BundleInstaller.PreviousFailure(root)!.Contains("访问被拒绝")&&BundleInstaller.PreviousFailure(root)!.Contains("错误 5"),"access denial does not masquerade as another running program");
    }
}
