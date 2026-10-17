using System.IO.Compression;
using System.Security.Cryptography;
using System.Text;
using Dnf90.Portable;

static string Hash(byte[] raw) => Convert.ToHexString(SHA256.HashData(raw)).ToLowerInvariant();
static void Assert(bool condition, string text) { if (!condition) throw new Exception(text); }
static void Reject(Action action)
{
    try { action(); } catch (Exception e) when (e is IOException or InvalidDataException or InvalidOperationException) { return; }
    throw new Exception("Unsafe or incomplete installation accepted");
}
static (byte[] Zip, Payload Payload, Dictionary<string, byte[]> Files) Fixture(string? extra = null)
{
    var files = new Dictionary<string, byte[]>
    {
        ["DNF90Launcher.exe"] = "MZ root fixture"u8.ToArray(),
        ["launcher.id"] = Encoding.UTF8.GetBytes(PortableCache.Marker + "\n"),
        ["app/DNF90.exe"] = "MZ GUI fixture"u8.ToArray(),
        ["app/DNF90.Core.exe"] = "MZ core fixture"u8.ToArray(),
        ["app/flutter_windows.dll"] = "MZ engine fixture"u8.ToArray(),
        ["app/data/app.so"] = "app fixture"u8.ToArray(),
        ["app/data/icudtl.dat"] = "icu fixture"u8.ToArray(),
        ["SHA256SUMS.txt"] = "test inventory"u8.ToArray()
    };
    if (extra is not null) files[extra] = "untrusted fixture"u8.ToArray();
    using var output = new MemoryStream();
    using (var zip = new ZipArchive(output, ZipArchiveMode.Create, true))
        foreach (var (name, raw) in files) { using var entry = zip.CreateEntry(name).Open(); entry.Write(raw); }
    var bytes = output.ToArray();
    return (bytes, new("0.2.0", bytes.Length, Hash(bytes), files.ToDictionary(x => x.Key, x => new FileDigest(x.Value.Length, Hash(x.Value)))), files);
}

var root = Path.Combine(Path.GetTempPath(), "dnf90-portable-tests-" + Guid.NewGuid().ToString("N"));
Directory.CreateDirectory(root);
var passed = 0;
void Test(string name, Action<string> action)
{
    var directory = Path.Combine(root, name); Directory.CreateDirectory(directory);
    action(directory); passed++; Console.WriteLine("PASS " + name);
}
var fixture = Fixture();
Prepared Prepare(string directory) => PortableCache.Prepare(new MemoryStream(fixture.Zip), fixture.Payload, directory);

Test("exact-first-extraction", directory =>
{
    var result = Prepare(directory);
    Assert(!result.Reused, "First extraction reported reused");
    foreach (var (name, raw) in fixture.Files)
        Assert(File.ReadAllBytes(Path.Combine(result.Directory, name)).SequenceEqual(raw), "Extracted bytes differ");
});
Test("repeated-entry-preserves-updated-app", directory =>
{
    var result = Prepare(directory);
    var exe = Path.Combine(result.Directory, "app", "DNF90.exe");
    File.WriteAllText(exe, "MZ later signed release fixture");
    var reused = PortableCache.Prepare(new MemoryStream(), fixture.Payload, directory);
    Assert(reused.Reused && File.ReadAllText(exe).Contains("later signed"), "Seed overwrote updated app");
});
Test("corrupt-seed-refused-before-installation", directory =>
{
    var bad = fixture.Zip.ToArray(); bad[^1] ^= 1;
    Reject(() => PortableCache.Prepare(new MemoryStream(bad), fixture.Payload, directory));
    Assert(!Directory.Exists(Path.Combine(directory, fixture.Payload.Sha256)), "Corrupt package installed");
});
Test("corrupt-bootstrap-not-repaired-over-user-data", directory =>
{
    var result = Prepare(directory); var file = Path.Combine(result.Directory, "DNF90Launcher.exe");
    File.WriteAllText(file, "corrupt fixture"); Reject(() => Prepare(directory));
    Assert(File.ReadAllText(file) == "corrupt fixture", "Damaged bootstrap silently overwritten");
});
Test("unrecognized-directory-untouched", directory =>
{
    var other = Path.Combine(directory, fixture.Payload.Sha256); Directory.CreateDirectory(other);
    File.WriteAllText(Path.Combine(other, "user.txt"), "preserve"); Reject(() => Prepare(directory));
    Assert(Directory.GetFileSystemEntries(other).Length == 1, "Unknown directory was modified");
});
Test("interrupted-stage-never-used", directory =>
{
    var stage = Path.Combine(directory, fixture.Payload.Sha256 + ".stage-interrupted"); Directory.CreateDirectory(stage);
    File.WriteAllText(Path.Combine(stage, "evidence"), "incomplete");
    Assert(!Prepare(directory).Reused && File.Exists(Path.Combine(stage, "evidence")), "Incomplete stage reused or deleted");
});
Test("missing-app-does-not-downgrade", directory =>
{
    var result = Prepare(directory);
    File.Delete(Path.Combine(result.Directory, "app/DNF90.exe"));
    Reject(() => Prepare(directory));
    Assert(!File.Exists(Path.Combine(result.Directory, "app/DNF90.exe")), "Missing app silently restored to seed");
});
Test("pending-update-remains-owned-by-bootstrap", directory =>
{
    var result = Prepare(directory);
    File.WriteAllText(Path.Combine(result.Directory, ".launcher-update.json"), "pending transaction fixture");
    Directory.Move(Path.Combine(result.Directory, "app"), Path.Combine(result.Directory, "previous-fixture"));
    Assert(Prepare(directory).Reused, "Recovery entry was blocked");
    Assert(!Directory.Exists(Path.Combine(result.Directory, "app")), "Portable host interfered with update recovery");
});
foreach (var name in new[] { "../outside", "app/CON.txt", "app/DNF90.EXE", "app/../outside", "private.key" })
{
    var bad = Fixture(name);
    Test("unsafe-path-" + passed, directory => Reject(() => PortableCache.Prepare(new MemoryStream(bad.Zip), bad.Payload, directory)));
}
Test("concurrent-prepare-one-installation", directory =>
{
    var results = Task.WhenAll(Enumerable.Range(0, 4).Select(_ => Task.Run(() => Prepare(directory)))).GetAwaiter().GetResult();
    Assert(results.Count(x => !x.Reused) == 1 && results.Select(x => x.Directory).Distinct().Count() == 1, "Concurrent install conflict");
});
Console.WriteLine($"{passed} portable checks passed; temporary fixtures only; no UI/game/network.");
