using System.Text;
using System.Text.Json;
using ServerS5A3;

internal static class ChannelHistoryChecks
{
    private const string Key = "CONFIG_CHARACTER_LATELY_GAMESERVER_INFO";

    internal static void Run(Action<bool, string> check, Action<Action, string> reject)
    {
        var spec = new ChannelHistoryCompatibility(936, Convert.ToBase64String("fixture-account"u8));
        var paired = new PlayerProfile("", "", "", "", "", 37001,
            "2af985b6e344e5fdbb78f6598d40924f152af924a156f5d45b671bed350a9675",
            "b86dd8fa5bcde40a445d9e494a9e1a89d0fc15ddd15d61041e4b97f78973d00f", "") { ChannelHistory = spec };
        NativeChannelHistory.ValidateCompatibility(paired, 936);
        check(true, "paired client can reach history preparation after strict file validation");
        NativeChannelHistory.ValidateCompatibility(paired with { ExeSha256 = "BC613E368E8FD49C50C35163F2ED8A28564FC23342701A5A6C22B839F3BF6B7A" }, 936);
        check(true, "original verified client retains history support");
        reject(() => NativeChannelHistory.ValidateCompatibility(paired with { ExeSha256 = new string('0', 64) }, 936), "unknown executable cannot reuse native history layout");
        reject(() => NativeChannelHistory.ValidateCompatibility(paired with { PvfSha256 = new string('0', 64) }, 936), "new executable does not permit an unverified PVF");
        reject(() => NativeChannelHistory.ValidateCompatibility(paired, 65001), "verified executable does not bypass native code page requirement");
        reject(() => NativeChannelHistory.ValidateCompatibility(paired with { ChannelHistory = spec with { WindowsCodePage = 65001 } }, 65001), "matching but unverified code page is rejected");
        reject(() => NativeChannelHistory.ValidateCompatibility(paired with { ChannelHistory = null }, 936), "missing native identity cannot pass compatibility validation");
        var channels = new[] { 10, 11 };
        var cold = NativeChannelHistory.Transform(Array.Empty<byte>(), spec, channels);
        check(Encoding.ASCII.GetString(cold) == "[fixture-account]\r\n" + Key + "=000000000A0000000A\r\n", "cold profile contains only the real landing history");
        check(NativeChannelHistory.Transform(cold, spec, channels).SequenceEqual(cold), "cold initialization is idempotent");
        var regionOne = NativeChannelHistory.Transform(cold, spec, channels, 1);
        check(Encoding.ASCII.GetString(regionOne) == "[fixture-account]\r\n" + Key + "=010000000A0000000B\r\n", "migrate directory without changing account section");
        check(NativeChannelHistory.Transform(regionOne, spec, channels, 1).SequenceEqual(regionOne), "migrated history is idempotent");
        check(NativeChannelHistory.Transform(regionOne, spec, channels, 0).SequenceEqual(cold), "verified old directory restores history during rollback");
        foreach (var region in new[] {-1, 250, 256})
            reject(() => NativeChannelHistory.Transform(cold, spec, channels, region), "unrepresentable native directory rejected");
        var profileJson = JsonSerializer.SerializeToUtf8Bytes(paired with { ChannelServerId = 1, PreviousChannelServerId = 0 }, CoreJson.Default.PlayerProfile);
        var restored = JsonSerializer.Deserialize(profileJson, CoreJson.Default.PlayerProfile)!;
        check(restored.ChannelServerId == 1 && restored.PreviousChannelServerId == 0, "AOT profile metadata preserves directory migration");
        var known = Encoding.ASCII.GetBytes("[fixture-account]\r\n" + Key + "=000000000B0000000B\r\nother=keep\r\n");
        check(NativeChannelHistory.Transform(known, spec, channels).SequenceEqual(known), "preserve another advertised remembered channel");
        var stale = Encoding.ASCII.GetBytes("[fixture-account]\r\n" + Key + "=000000006300000063\r\nother=keep\r\n");
        check(Encoding.ASCII.GetString(NativeChannelHistory.Transform(stale, spec, channels)).EndsWith("other=keep\r\n"), "stale history repaired without changing adjacent preference");
        var corrupt = Encoding.ASCII.GetBytes("[fixture-account]\r\n" + Key + "=000000000A000000FF\r\n");
        check(NativeChannelHistory.Transform(corrupt, spec, channels).SequenceEqual(cold), "invalid Windows profile checksum repaired");
        var other = Encoding.Latin1.GetBytes("[DNF COMMON]\r\nwidth=800\r\n[other-account]\r\n" + Key + "=private\r\n;\x81\xff");
        check(NativeChannelHistory.Transform(other, spec, channels).AsSpan().StartsWith(other), "all unrelated raw bytes and account sections preserved");
        var missing = Encoding.ASCII.GetBytes("[fixture-account]\r\nother=keep\r\n[unrelated]\r\nx=y\r\n");
        check(Encoding.ASCII.GetString(NativeChannelHistory.Transform(missing, spec, channels)).Contains("[fixture-account]\r\n"+Key+"=000000000A0000000A\r\nother=keep"), "insert within the correct existing section");
        reject(() => NativeChannelHistory.Transform(cold, spec, new[] { 11 }), "never invent an absent landing channel");
        reject(() => NativeChannelHistory.Transform(Encoding.Unicode.GetBytes("[profile]"), spec, channels), "unknown Unicode file representation preserved");
        reject(() => NativeChannelHistory.Transform(new byte[256*1024+1], spec, channels), "oversized profile bounded");
        reject(() => NativeChannelHistory.Transform(cold.Concat(cold).ToArray(), spec, channels), "duplicate section rejected");
        reject(() => NativeChannelHistory.Transform(Encoding.ASCII.GetBytes("[fixture-account]\r\n"+Key+"=a\r\n"+Key+"=b"), spec, channels), "duplicate history key rejected");
        reject(() => NativeChannelHistory.Transform(Array.Empty<byte>(), spec with { AccountSectionBase64=Convert.ToBase64String("bad]\r\n[other"u8) }, channels), "section injection rejected");
        if (OperatingSystem.IsWindows()) FileChecks(spec, check, reject);
    }

    private static void FileChecks(ChannelHistoryCompatibility spec, Action<bool, string> check, Action<Action, string> reject)
    {
        var directory = Path.Combine(Path.GetTempPath(), "servers5a3-history-"+Guid.NewGuid().ToString("N"));
        Directory.CreateDirectory(directory);
        try
        {
            var path = Path.Combine(directory, "DNF.cfg"); var channels = new[] { 10, 11 };
            reject(() => NativeChannelHistory.PrepareFile(path, spec, channels, () => true), "running game prevents a new configuration write");
            check(!File.Exists(path), "running-game rejection leaves missing file missing");
            check(NativeChannelHistory.PrepareFile(path, spec, channels, () => false), "fresh configuration file created");
            check(NativeChannelHistory.TryReadNative(path, spec, out var server, out var channel) && server==0 && channel==10, "native Windows API reads landing identity and checksum");
            check(!NativeChannelHistory.PrepareFile(path, spec, channels, () => true), "valid configuration needs no write while a game runs");
            var zeroBytes = File.ReadAllBytes(path);
            reject(() => NativeChannelHistory.PrepareFile(path, spec, channels, () => true, 1), "running game prevents directory migration");
            check(File.ReadAllBytes(path).SequenceEqual(zeroBytes), "blocked migration leaves exact bytes intact");
            check(NativeChannelHistory.PrepareFile(path, spec, channels, () => false, 1), "directory migration writes atomically");
            check(NativeChannelHistory.TryReadNative(path, spec, out server, out channel) && server == 1 && channel == 10, "native Windows API verifies migrated identity and checksum");
            check(!NativeChannelHistory.PrepareFile(path, spec, channels, () => false, 1), "migrated native file needs no repeated write");
            check(Directory.GetFiles(directory, "*.bak").Any(p => File.ReadAllBytes(p).SequenceEqual(zeroBytes)), "migration retains exact original file backup");
            var original = "[DNF COMMON]\r\nwidth=800\r\n"u8.ToArray(); File.WriteAllBytes(path,original);
            check(NativeChannelHistory.PrepareFile(path, spec, channels, () => false), "existing profile amended atomically");
            var backups = Directory.GetFiles(directory,"*.bak");
            check(backups.Length==2 && backups.Any(p => File.ReadAllBytes(p).SequenceEqual(original)), "exact original profile retained as backup");
            File.WriteAllBytes(path, original);
            var calls=0;
            reject(() => NativeChannelHistory.PrepareFile(path,spec,channels,()=> { if (++calls==2) File.WriteAllText(path,"concurrent change"); return false; }), "concurrent profile change blocks replacement");
            check(File.ReadAllText(path)=="concurrent change", "concurrent writer's data survives");
            check(Directory.GetFiles(directory,"*.tmp").Length==0, "private temporary files cleaned");
        }
        finally { Directory.Delete(directory,true); }
    }

    internal static void PrivateProfileFixture(string path)
    {
        var profile = JsonSerializer.Deserialize<PlayerProfile>(File.ReadAllText(path), PlayerApi.Json)!;
        var passed=0;
        void Check(bool yes,string name) { if(!yes) throw new Exception(name); passed++; }
        void Reject(Action action,string name) { try { action(); } catch { passed++; return; } throw new Exception(name); }
        FileChecks(profile.ChannelHistory ?? throw new Exception("Missing compatibility input"),Check,Reject);
        Console.WriteLine($"PASS {passed} isolated release-profile history checks; no player configuration or game process touched");
    }
}
