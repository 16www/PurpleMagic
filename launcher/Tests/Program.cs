using System.IO.Compression;
using System.Net.Security;
using System.Security.Cryptography;
using System.Security.Cryptography.X509Certificates;
using System.Text.Json;
using ServerS5A3;

if (args.Length == 2 && args[0] == "--native-gate-fixture") { NativeGateChecks.Fixture(args[1]); return; }
if (args.Length == 2 && args[0] == "--history-profile") { ChannelHistoryChecks.PrivateProfileFixture(args[1]); return; }
if (args.Length == 3 && args[0] == "--update-instance-fixture") { UpdateChecks.InstanceFixture(args[1], args[2]); return; }

var passed = 0;
void Check(bool condition, string name) { if (!condition) throw new Exception(name); passed++; }
void Reject(Action action, string name) { try { action(); } catch { passed++; return; } throw new Exception(name); }
using var rsa = RSA.Create(2048);
var publicKey = rsa.ExportSubjectPublicKeyInfoPem();
byte[] Sign<T>(T value) { var raw = JsonSerializer.SerializeToUtf8Bytes(value); return JsonSerializer.SerializeToUtf8Bytes(new SignedEnvelope(Convert.ToBase64String(raw), Convert.ToBase64String(rsa.SignData(raw, HashAlgorithmName.SHA256, RSASignaturePadding.Pss)))); }
var settings = new UpdateSettings(true, "https://example.invalid/release.json", "notice");
var envelope = Sign(settings);
Check(SignedUpdate.Verify<UpdateSettings>(envelope, publicKey) == settings, "signed settings");
using (var other = RSA.Create(2048)) Reject(() => SignedUpdate.Verify<UpdateSettings>(envelope, other.ExportSubjectPublicKeyInfoPem()), "foreign signer");
var tampered = JsonSerializer.Deserialize<SignedEnvelope>(envelope)! with { Payload = Convert.ToBase64String("{}"u8) };
Reject(() => SignedUpdate.Verify<UpdateSettings>(JsonSerializer.SerializeToUtf8Bytes(tampered), publicKey), "changed payload");
Reject(() => SignedUpdate.RequireHttps("http://example.invalid"), "plain update");
Reject(() => GameContract.Arguments("bad-token", "127.0.0.1", 7001), "invalid token");
Check(GameContract.Arguments("de509f65000000000000000000000000", "10.1.2.3", 37001).Split('?').Length == 16, "native argument layout");
var temporary = Path.Combine(Path.GetTempPath(), "servers5a3-update-" + Guid.NewGuid().ToString("N")); Directory.CreateDirectory(temporary);
try {
    var exe = Path.Combine(temporary, "ServerS5A3.exe"); File.WriteAllText(exe, "MZold");
    string Package(string entry) { var path = Path.Combine(temporary, Guid.NewGuid()+".zip"); using var zip = ZipFile.Open(path, ZipArchiveMode.Create); using var output = new StreamWriter(zip.CreateEntry(entry).Open()); output.Write("MZnew"); return path; }
    ReleaseManifest Manifest(string path) => new("0.2.0", "https://example.invalid/package.zip", new FileInfo(path).Length, Convert.ToHexString(SHA256.HashData(File.ReadAllBytes(path))));
    var good = Package("ServerS5A3.exe"); var release = Manifest(good);
    Reject(() => SignedUpdate.ValidateManifest(release with { Version = "0.0.1" }, new Version(0,1,0)), "downgrade");
    Reject(() => SignedUpdate.Apply(good, release with { Sha256 = new string('0',64) }, exe), "wrong hash");
    Check(File.ReadAllText(exe)=="MZold", "hash failure preserves old");
    if (OperatingSystem.IsWindows()) {
        using var locked = new FileStream(exe, FileMode.Open, FileAccess.Read, FileShare.None);
        Reject(() => SignedUpdate.Apply(good, release, exe, TimeSpan.Zero), "locked launcher replacement rejected");
    }
    Check(File.ReadAllText(exe)=="MZold", "locked replacement preserves old");
    foreach (var entry in new[] { "../ServerS5A3.exe", "/ServerS5A3.exe", "Other.exe" }) {
        var evil = Package(entry); Reject(() => SignedUpdate.Apply(evil, Manifest(evil), exe), "unsafe package path");
    }
    SignedUpdate.Apply(good, release, exe); Check(File.ReadAllText(exe)=="MZnew" && File.ReadAllText(exe+".bak")=="MZold", "replace and backup");
    SignedUpdate.Rollback(exe); Check(File.ReadAllText(exe)=="MZold", "rollback");
    var pref = Path.Combine(temporary, "player.json"); new PlayerPreferences { Account="tester" }.Save(pref);
    Check(!File.ReadAllText(pref).Contains("password", StringComparison.OrdinalIgnoreCase), "no password persistence");
    using var certKey = RSA.Create(2048);
    var request = new CertificateRequest("CN=test", certKey, HashAlgorithmName.SHA256, RSASignaturePadding.Pkcs1);
    using var cert = request.CreateSelfSigned(DateTimeOffset.UtcNow.AddDays(-1), DateTimeOffset.UtcNow.AddDays(1));
    var profile = new PlayerProfile("https://192.0.2.1", Convert.ToHexString(SHA256.HashData(cert.RawData)),publicKey,"","",37001,"","","");
    Check(PlayerApi.CertificateAllowed(new Uri(profile.ApiUrl),cert,SslPolicyErrors.RemoteCertificateChainErrors,profile), "exact pinned certificate");
    Check(!PlayerApi.CertificateAllowed(new Uri(profile.ApiUrl),cert,SslPolicyErrors.RemoteCertificateNameMismatch,profile), "wrong certificate name");
    Check(!PlayerApi.CertificateAllowed(new Uri(profile.ApiUrl),cert,SslPolicyErrors.None,profile with {CertificateSha256=new string('0',64)}), "changed certificate");
    Check(!PlayerApi.CertificateAllowed(new Uri("https://other.invalid"),cert,SslPolicyErrors.RemoteCertificateChainErrors,profile), "other host normal validation");
} finally { Directory.Delete(temporary,true); }
if (OperatingSystem.IsWindows()) NativeGateChecks.Run(Check, Reject);
await ChannelProbeChecks.Run(Check, Reject);
ChannelHistoryChecks.Run(Check, Reject);
UpdateChecks.Run(Check, Reject);
GuildCompatibilityChecks.Run(Check, Reject);
Console.WriteLine($"PASS {passed} launcher safety checks");
