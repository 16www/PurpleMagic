// Explicitly headless transport/update verification. Never launches a process.
using System.Text.Json;
using ServerS5A3;

if (args.Length is < 1 or > 2) throw new ArgumentException("Expected private profile path and optional client directory or --channel-only");
var profile = JsonSerializer.Deserialize<PlayerProfile>(File.ReadAllText(args[0]), PlayerApi.Json)!;
using var api = new PlayerApi(profile);
await api.CheckStatus();
Console.WriteLine("PASS C# TLS certificate pin and remote status");
var channels = await ChannelProbe.Check(profile);
Console.WriteLine($"PASS native channel bootstrap: server {channels.ServerId}; {channels.Channels} rows; {channels.TestedPorts} game ports");
if (args.Length == 2 && args[1] == "--channel-only") return;
var settings = SignedUpdate.Verify<UpdateSettings>(await SignedUpdate.FetchSmall(api.Http, profile.SettingsUrl), profile.UpdatePublicKey);
if (settings.Enabled) {
    var release = SignedUpdate.Verify<ReleaseManifest>(await SignedUpdate.FetchSmall(api.Http, settings.ManifestUrl), profile.UpdatePublicKey);
    var path = Path.Combine(Path.GetTempPath(), "servers5a3-probe-" + Guid.NewGuid().ToString("N") + ".zip");
    try {
        var last = -1;
        await SignedUpdate.Download(api.Http, release, path, new Progress<double>(p => {
            var step = (int)(p * 10); if (step > last) { last = step; Console.WriteLine($"Download {step * 10}%"); }
        }));
        Console.WriteLine("PASS signed remote package download and SHA256: " + release.Version);
    } finally { File.Delete(path); }
}
await GameContract.VerifyClient(args.Length == 2 ? args[1] : profile.DefaultClientDirectory, profile);
Console.WriteLine("PASS current DNF.exe and PVF match; no game was launched");
