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
        var v106 = paired with {
            ExeSha256 = "6fd3183de3cff52fa2feb5a5327ca43855cabbc6b0b63d920a87ab8ddf316da5",
            PvfSha256 = "ad7d05c70421278f7385729bcba3e81e37d99d0f3df85a87a9f0dc19c92504cb"
        };
        NativeChannelHistory.ValidateCompatibility(v106, 936);
        check(true, "verified v106 EXE and PVF can initialize channel history");
        NativeChannelHistory.ValidateCompatibility(v106 with { ExeSha256 = v106.ExeSha256.ToUpperInvariant(), PvfSha256 = v106.PvfSha256.ToUpperInvariant() }, 936);
        check(true, "verified pair comparison is case insensitive");
        var guildV107 = v106 with { ExeSha256 = "eeb8de2282001dc012eaf5b6f3df5a920539ab93cc3d07a626d9b478a301c9dd" };
        NativeChannelHistory.ValidateCompatibility(guildV107, 936);
        check(true, "guild v107 final EXE and V106 PVF can initialize channel history");
        reject(() => NativeChannelHistory.ValidateCompatibility(guildV107 with { PvfSha256 = paired.PvfSha256 }, 936), "guild release cannot mix with legacy PVF");
        reject(() => NativeChannelHistory.ValidateCompatibility(guildV107 with { PvfSha256 = "369d072b82c715aea4e02813d89ad9225686983e147bc7be0ee0cae2e296bb06" }, 936), "guild release requires current approved PVF, not historical test PVF");
        reject(() => NativeChannelHistory.ValidateCompatibility(guildV107, 65001), "guild release retains native code page guard");
        reject(() => NativeChannelHistory.ValidateCompatibility(v106 with { PvfSha256 = paired.PvfSha256 }, 936), "new EXE cannot use legacy PVF history approval");
        reject(() => NativeChannelHistory.ValidateCompatibility(paired with { PvfSha256 = v106.PvfSha256 }, 936), "legacy EXE cannot use new PVF history approval");
        reject(() => NativeChannelHistory.ValidateCompatibility(v106, 65001), "v106 retains code page guard");
        var seria = v106 with { ExeSha256 = "eeb8de2282001dc012eaf5b6f3df5a920539ab93cc3d07a626d9b478a301c9dd", PvfSha256 = "8f05ba10914d8d1c37113245402395e8ce80920e455a8af03f4a2cafd3b64df8" };
        NativeChannelHistory.ValidateCompatibility(seria, 936);
        check(true, "approved shop-only 8f05ba10914d8d1c37113245402395e8ce80920e455a8af03f4a2cafd3b64df8 preserves channel history compatibility");
        reject(() => NativeChannelHistory.ValidateCompatibility(seria with { ExeSha256 = v106.ExeSha256 }, 936), "shop 8f05ba10914d8d1c37113245402395e8ce80920e455a8af03f4a2cafd3b64df8 cannot authorize unapproved eeb8de2282001dc012eaf5b6f3df5a920539ab93cc3d07a626d9b478a301c9dd pairing");
        var v110 = seria with { ExeSha256 = "fc4fa42ce9fd47307992fb6808840e68f84d89285570c383a100918aa641c3de" };
        NativeChannelHistory.ValidateCompatibility(v110, 936);
        check(true, "verified v110 retains guild and channel code with the approved owned PVF");
        reject(() => NativeChannelHistory.ValidateCompatibility(v110 with { PvfSha256 = v106.PvfSha256 }, 936), "v110 approval is limited to the owned PVF pair");
        reject(() => NativeChannelHistory.ValidateCompatibility(v110, 65001), "v110 retains native code page validation");
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
        var eleven = NativeChannelHistory.Transform(Array.Empty<byte>(), spec, new[] { 11 }, 1);
        check(Encoding.ASCII.GetString(eleven) == "[fixture-account]\r\n" + Key + "=010000000B0000000C\r\n", "script-first directory initializes verified channel 11 and checksum");
        check(NativeChannelHistory.Transform(cold, spec, new[] { 11 }, 1).SequenceEqual(eleven), "old channel 10 migrates to advertised channel 11");
        check(NativeChannelHistory.Transform(eleven, spec, new[] { 11 }, 1).SequenceEqual(eleven), "channel 11 initialization is idempotent");
        check(NativeChannelHistory.Transform(eleven, spec, new[] { 10 }, 1).SequenceEqual(regionOne), "rollback directory restores advertised channel 10");
        check(NativeChannelHistory.Transform(Array.Empty<byte>(), spec, new[] { 11, 10 }).SequenceEqual(cold), "prefer channel 10 regardless of directory order");
        foreach (var unavailable in new[] { Array.Empty<int>(), new[] { 12 }, new[] { 19, 251, 252, 253 } })
            reject(() => NativeChannelHistory.Transform(cold, spec, unavailable), "never invent an absent verified landing channel");
        reject(() => NativeChannelHistory.Transform(Encoding.Unicode.GetBytes("[profile]"), spec, channels), "unknown Unicode file representation preserved");
        reject(() => NativeChannelHistory.Transform(new byte[256*1024+1], spec, channels), "oversized profile bounded");
        reject(() => NativeChannelHistory.Transform(cold.Concat(cold).ToArray(), spec, channels), "duplicate section rejected");
        reject(() => NativeChannelHistory.Transform(Encoding.ASCII.GetBytes("[fixture-account]\r\n"+Key+"=a\r\n"+Key+"=b"), spec, channels), "duplicate history key rejected");
        reject(() => NativeChannelHistory.Transform(Array.Empty<byte>(), spec with { AccountSectionBase64=Convert.ToBase64String("bad]\r\n[other"u8) }, channels), "section injection rejected");
        if (OperatingSystem.IsWindows()) { FileChecks(spec, check, reject); ElevenFileChecks(spec, check, reject); }
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

    private static void ElevenFileChecks(ChannelHistoryCompatibility spec, Action<bool, string> check, Action<Action, string> reject)
    {
        var directory = Path.Combine(Path.GetTempPath(), "servers5a3-history-eleven-" + Guid.NewGuid().ToString("N"));
        Directory.CreateDirectory(directory);
        try
        {
            var path = Path.Combine(directory, "DNF.cfg"); var channels = new[] { 11 };
            reject(() => NativeChannelHistory.PrepareFile(path, spec, channels, () => true, 1), "running game blocks cold channel 11 write");
            check(!File.Exists(path), "blocked channel 11 initialization leaves file absent");
            check(NativeChannelHistory.PrepareFile(path, spec, channels, () => false, 1), "cold channel 11 file written");
            check(NativeChannelHistory.TryReadNative(path, spec, out var server, out var channel) && server == 1 && channel == 11, "native Windows reader verifies channel 11 and checksum");
            check(!NativeChannelHistory.PrepareFile(path, spec, channels, () => true, 1), "valid channel 11 history stays unchanged during game");
            var original = NativeChannelHistory.Transform("[DNF COMMON]\r\nwidth=800\r\n"u8.ToArray(), spec, new[] { 10 }, 1);
            File.WriteAllBytes(path, original);
            reject(() => NativeChannelHistory.PrepareFile(path, spec, channels, () => true, 1), "running game blocks channel 10 to 11 migration");
            check(File.ReadAllBytes(path).SequenceEqual(original), "blocked landing migration preserves original bytes");
            check(NativeChannelHistory.PrepareFile(path, spec, channels, () => false, 1), "channel 10 to 11 migration replaces atomically");
            check(NativeChannelHistory.TryReadNative(path, spec, out server, out channel) && server == 1 && channel == 11, "native reader verifies migrated channel 11");
            check(File.ReadAllBytes(path).AsSpan().StartsWith("[DNF COMMON]\r\nwidth=800\r\n"u8), "channel migration preserves preferences");
            check(Directory.GetFiles(directory, "*.bak").Any(p => File.ReadAllBytes(p).SequenceEqual(original)), "landing migration backs up exact original bytes");
            check(!NativeChannelHistory.PrepareFile(path, spec, channels, () => false, 1), "repeated channel 11 preparation does not rewrite");
        }
        finally { Directory.Delete(directory, true); }
    }

    internal static void PrivateProfileFixture(string path)
    {
        var profile = JsonSerializer.Deserialize<PlayerProfile>(File.ReadAllText(path), PlayerApi.Json)!;
        var passed=0;
        void Check(bool yes,string name) { if(!yes) throw new Exception(name); passed++; }
        void Reject(Action action,string name) { try { action(); } catch { passed++; return; } throw new Exception(name); }
        FileChecks(profile.ChannelHistory ?? throw new Exception("Missing compatibility input"),Check,Reject);
        ElevenFileChecks(profile.ChannelHistory!,Check,Reject);
        Console.WriteLine($"PASS {passed} isolated release-profile history checks; no player configuration or game process touched");
    }
}
