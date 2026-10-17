using System.Security.Cryptography;
using ServerS5A3;

internal static class GuildCompatibilityChecks
{
    internal static void Run(Action<bool, string> check, Action<Action, string> reject)
    {
        var profile = new PlayerProfile("", "", "", "", "", 0, NativeGuildCompatibility.Original, NativeGuildCompatibility.Pvf, "");
        check(NativeGuildCompatibility.AcceptsDerivative(profile, NativeGuildCompatibility.Patched), "exact approved guild derivative accepted");
        check(!NativeGuildCompatibility.AcceptsDerivative(profile with { ExeSha256 = new string('0', 64) }, NativeGuildCompatibility.Patched), "derivative cannot bypass another EXE contract");
        check(!NativeGuildCompatibility.AcceptsDerivative(profile with { PvfSha256 = new string('0', 64) }, NativeGuildCompatibility.Patched), "derivative cannot bypass another PVF contract");
        check(!NativeGuildCompatibility.AcceptsDerivative(profile, new string('0', 64)), "unknown EXE still refused");
        reject(() => NativeGuildCompatibility.Transform(new byte[32]), "truncated EXE refused");
        var fixture = Environment.GetEnvironmentVariable("DNF_GUILD_CLIENT_FIXTURE");
        if (string.IsNullOrWhiteSpace(fixture)) { Console.WriteLine("SKIP exact private client patch fixture (DNF_GUILD_CLIENT_FIXTURE)"); return; }
        var original = File.ReadAllBytes(fixture);
        var patched = NativeGuildCompatibility.Transform(original);
        check(Convert.ToHexString(SHA256.HashData(patched)) == NativeGuildCompatibility.Patched, "real client derivative digest");
        const int bitmap = 0x6a83558, gate = 0x6a83610;
        static bool Allows(byte[] bytes, int op) => op <= BitConverter.ToInt32(bytes, gate + 6) && (bytes[bitmap + op / 8] & (1 << (op % 8))) != 0;
        var reviewed = new HashSet<int> { 67,68,127,139,140,141,156,347,348,350,352,685,686,687,739,741,742,744,745,762,1450,1452,1453 };
        check(!Allows(original, 156) && Allows(patched, 156), "name check previously discarded, now delivered");
        for (int op = 0; op <= ushort.MaxValue; op++)
            if (Allows(patched, op) != (Allows(original, op) || reviewed.Contains(op))) throw new Exception("unreviewed opcode change");
        check(true, "all 65536 opcode decisions reviewed, unknown still blocked");
        original[123] ^= 1;
        reject(() => NativeGuildCompatibility.Transform(original), "unrelated tampering rejected before patch");
        original[123] ^= 1;
        var directory = Path.Combine(Path.GetTempPath(), "guild-client-test-" + Guid.NewGuid().ToString("N")); Directory.CreateDirectory(directory);
        try
        {
            var target = Path.Combine(directory, "DNF.exe"); File.WriteAllBytes(target, original);
            reject(() => NativeGuildCompatibility.Prepare(directory, profile, () => true), "running game prevents patch");
            check(Directory.GetFiles(directory).Length == 1, "running game writes nothing");
            int calls = 0;
            reject(() => NativeGuildCompatibility.Prepare(directory, profile, () => ++calls > 1), "game starting during preparation prevents patch");
            check(Directory.GetFiles(directory).Length == 1, "aborted stage cleaned, original preserved");
            if (OperatingSystem.IsWindows())
            {
                using var locked = new FileStream(target, FileMode.Open, FileAccess.Read, FileShare.Read);
                reject(() => NativeGuildCompatibility.Prepare(directory, profile, () => false), "mapped EXE style delete lock prevents replacement");
            }
            check(NativeGuildCompatibility.Prepare(directory, profile, () => false), "real isolated EXE patched atomically");
            check(File.ReadAllBytes(target).AsSpan().SequenceEqual(patched), "installed bytes match exact derivative");
            var backup = Directory.GetFiles(directory, "*.bak").Single();
            check(File.ReadAllBytes(backup).AsSpan().SequenceEqual(original), "original backup exact");
            check(!NativeGuildCompatibility.Prepare(directory, profile, () => true), "repeat while running is read-only no-op");
            File.Replace(backup, target, null);
            check(File.ReadAllBytes(target).AsSpan().SequenceEqual(original), "atomic rollback restores original");
        }
        finally { Directory.Delete(directory, true); }
    }
}
