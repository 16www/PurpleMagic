using System.IO.Compression;
using System.Diagnostics;
using System.Security.Cryptography;
using System.Text.Json;

namespace ServerS5A3;

public sealed record SignedEnvelope(string Payload, string Signature);
public sealed record UpdateSettings(bool Enabled, string ManifestUrl, string Notice);
public sealed record ReleaseManifest(string Version, string Url, long Size, string Sha256);

public static class SignedUpdate
{
    public const long MaxPackageBytes = 300 * 1024 * 1024;
    public static T Verify<T>(byte[] envelope, string publicKey)
    {
        if (envelope.Length > 65536) throw new InvalidDataException("更新清单过大。");
        var signed = JsonSerializer.Deserialize(envelope, CoreJson.Default.SignedEnvelope) ?? throw new InvalidDataException();
        var payload = Convert.FromBase64String(signed.Payload);
        using var rsa = RSA.Create(); rsa.ImportFromPem(publicKey);
        if (!rsa.VerifyData(payload, Convert.FromBase64String(signed.Signature), HashAlgorithmName.SHA256, RSASignaturePadding.Pss))
            throw new InvalidDataException("更新签名验证失败，已保留当前版本。");
        return JsonSerializer.Deserialize(payload, (System.Text.Json.Serialization.Metadata.JsonTypeInfo<T>)PlayerApi.Json.GetTypeInfo(typeof(T))) ?? throw new InvalidDataException();
    }
    public static async Task<byte[]> FetchSmall(HttpClient http, string url)
    {
        RequireHttps(url);
        using var response = await http.GetAsync(url, HttpCompletionOption.ResponseHeadersRead);
        response.EnsureSuccessStatusCode();
        using var input = await response.Content.ReadAsStreamAsync();
        using var output = new MemoryStream(); var buffer = new byte[4096]; int read;
        while ((read = await input.ReadAsync(buffer)) > 0)
        {
            if (output.Length + read > 65536) throw new InvalidDataException("更新清单过大。");
            output.Write(buffer, 0, read);
        }
        return output.ToArray();
    }
    public static void RequireHttps(string url)
    {
        if (!Uri.TryCreate(url, UriKind.Absolute, out var uri) || uri.Scheme != "https" || !string.IsNullOrEmpty(uri.UserInfo))
            throw new InvalidDataException("更新地址必须使用 HTTPS。");
    }
    public static void ValidateManifest(ReleaseManifest release, Version current)
    {
        RequireHttps(release.Url);
        if (!Version.TryParse(release.Version, out var next) || next <= current || release.Size is < 1 or > MaxPackageBytes
            || !System.Text.RegularExpressions.Regex.IsMatch(release.Sha256, "^[a-fA-F0-9]{64}$"))
            throw new InvalidDataException("更新版本或文件信息无效。");
    }
    public static async Task Download(HttpClient http, ReleaseManifest release, string target, IProgress<double>? progress = null)
    {
        RequireHttps(release.Url);
        using var cancel = new CancellationTokenSource(TimeSpan.FromMinutes(30));
        using var response = await http.GetAsync(release.Url, HttpCompletionOption.ResponseHeadersRead, cancel.Token);
        response.EnsureSuccessStatusCode();
        try
        {
            await using var input = await response.Content.ReadAsStreamAsync(cancel.Token);
            await using var output = new FileStream(target, FileMode.CreateNew, FileAccess.Write, FileShare.None);
            var buffer = new byte[65536]; long total = 0; int read;
            while ((read = await input.ReadAsync(buffer, cancel.Token)) > 0)
            {
                total += read;
                if (total > release.Size || total > MaxPackageBytes) throw new InvalidDataException("更新文件大小不符。");
                await output.WriteAsync(buffer.AsMemory(0, read), cancel.Token);
                progress?.Report((double)total / release.Size);
            }
            if (total != release.Size) throw new InvalidDataException("更新下载不完整。");
        }
        catch { File.Delete(target); throw; }
        try { VerifyPackage(target, release); } catch { File.Delete(target); throw; }
    }
    public static void VerifyPackage(string package, ReleaseManifest release)
    {
        using var stream = File.OpenRead(package);
        if (stream.Length != release.Size || !Convert.ToHexString(SHA256.HashData(stream)).Equals(release.Sha256, StringComparison.OrdinalIgnoreCase))
            throw new InvalidDataException("更新校验失败，已保留当前版本。");
    }
    public static void Apply(string package, ReleaseManifest release, string executable, TimeSpan? lockTimeout = null)
    {
        VerifyPackage(package, release);
        using var archive = ZipFile.OpenRead(package);
        if (archive.Entries.Count != 1 || archive.Entries[0].FullName != "ServerS5A3.exe" || archive.Entries[0].Length > MaxPackageBytes)
            throw new InvalidDataException("更新包内容无效。");
        var stage = executable + "." + Guid.NewGuid().ToString("N") + ".new";
        try
        {
            archive.Entries[0].ExtractToFile(stage, false);
            using (var input = File.OpenRead(stage))
            { if (input.ReadByte() != 'M' || input.ReadByte() != 'Z') throw new InvalidDataException("更新程序格式无效。"); }
            ReplaceWhenAvailable(stage, executable, executable + ".bak", lockTimeout ?? TimeSpan.FromSeconds(15));
        }
        finally { File.Delete(stage); }
    }
    public static void Rollback(string executable, TimeSpan? lockTimeout = null)
    {
        if (!File.Exists(executable + ".bak")) throw new InvalidOperationException("没有可恢复的旧版本。");
        ReplaceWhenAvailable(executable + ".bak", executable, executable + ".failed", lockTimeout ?? TimeSpan.FromSeconds(15));
    }

    private static void ReplaceWhenAvailable(string source, string target, string backup, TimeSpan timeout)
    {
        var timer = Stopwatch.StartNew();
        while (true)
        {
            try { File.Replace(source, target, backup, true); return; }
            catch (Exception error) when (OperatingSystem.IsWindows() &&
                (error is IOException or UnauthorizedAccessException) &&
                ((error.HResult & 0xffff) is 5 or 32 or 33) && timer.Elapsed < timeout)
            {
                // Retry only Windows access/sharing/lock failures, never a bad package
                // or a missing path. The original process has already exited.
                Thread.Sleep((int)Math.Min(100, Math.Max(1, (timeout - timer.Elapsed).TotalMilliseconds)));
            }
        }
    }
}
