using System.IO.Compression;
using System.Net;
using System.Net.Http.Headers;
using System.Security.Cryptography;
using ServerS5A3;

namespace Dnf90.Launcher;

public static class ContentUpdater
{
    public static HttpClient Client(ContentRelease source)
    {
        var origin = new Uri(source.Endpoint);
        var handler = new HttpClientHandler { UseProxy = false, AllowAutoRedirect = false, AutomaticDecompression = DecompressionMethods.None };
        if (source.CertificateSha256 != "") handler.ServerCertificateCustomValidationCallback = (request, cert, _, _) =>
            request.RequestUri is { } uri && uri.Scheme == "https" && uri.Host == origin.Host && uri.Port == origin.Port && cert != null &&
            DateTime.UtcNow >= cert.NotBefore.ToUniversalTime() && DateTime.UtcNow <= cert.NotAfter.ToUniversalTime() && Trust.Digest(cert.RawData).Equals(source.CertificateSha256, StringComparison.OrdinalIgnoreCase);
        return new HttpClient(handler) { Timeout = Timeout.InfiniteTimeSpan };
    }
    public static async Task<ContentFile[]> Plan(string directory, ContentRelease release, CancellationToken cancel, string? reportPath = null)
    {
        var result = new List<ContentFile>();
        var report = new System.Text.StringBuilder();
        report.AppendLine("客户端资源检查（只按实际文件内容决定下载）");
        report.AppendLine("目录：" + Path.GetFullPath(directory));
        report.AppendLine("版本：" + release.Version);
        report.AppendLine("文件\t检查结果\t本地字节\t预期字节\t本地SHA256\t预期SHA256");
        foreach (var file in release.Files)
        {
            cancel.ThrowIfCancellationRequested();
            var target = SafeFiles.Target(directory, file.Path);
            long actualSize = -1; string actualHash = "";
            if (File.Exists(target))
            {
                await using var input = new FileStream(target, FileMode.Open, FileAccess.Read, FileShare.Read, 65536, true);
                actualSize = input.Length;
                actualHash = Convert.ToHexString(await SHA256.HashDataAsync(input, cancel)).ToLowerInvariant();
            }
            var matches = actualSize == file.Length && actualHash.Equals(file.Sha256, StringComparison.OrdinalIgnoreCase);
            var reason = matches ? "一致，无需下载" : actualSize < 0 ? "文件缺失" : actualSize != file.Length ? "文件大小不同" : "文件摘要不同";
            report.AppendLine($"{file.Path}\t{reason}\t{actualSize}\t{file.Length}\t{actualHash}\t{file.Sha256}");
            if (matches) continue;
            if (!file.Downloadable) throw new InvalidDataException($"缺少或不匹配的基线文件：{file.Path}。请使用配套完整客户端。");
            result.Add(file);
        }
        report.AppendLine($"需要下载：{result.Count} 项，{result.Sum(TransferSize)} 字节（压缩传输量）。");
        // A local diagnostic is best effort; it must never turn a matching file
        // into a download or expose a transient provider URL/OAuth credential.
        try
        {
            if (reportPath is not null) {
                SafeFiles.NoLinks(reportPath); Directory.CreateDirectory(Path.GetDirectoryName(Path.GetFullPath(reportPath))!);
                SafeFiles.Atomic(reportPath, System.Text.Encoding.UTF8.GetBytes(report.ToString()));
            }
        }
        catch (IOException) { }
        catch (UnauthorizedAccessException) { }
        return result.ToArray();
    }
    public static long TransferSize(ContentFile f) => f.Compression == "gzip" ? f.BlobLength : f.Length;
    public static async Task Download(HttpClient client, Uri url, long size, string hash, string partial, Action<long>? progress, CancellationToken cancel)
    {
        SignedUpdate.RequireHttps(url.AbsoluteUri); SafeFiles.NoLinks(partial); Directory.CreateDirectory(Path.GetDirectoryName(partial)!);
        if (await SafeFiles.Matches(partial, size, hash, cancel)) { progress?.Invoke(size); return; }
        long offset = File.Exists(partial) ? new FileInfo(partial).Length : 0;
        if (offset >= size) { File.Delete(partial); offset = 0; }
        using var request = new HttpRequestMessage(HttpMethod.Get, url);
        request.Headers.AcceptEncoding.Add(new StringWithQualityHeaderValue("identity"));
        if (offset > 0) request.Headers.Range = new RangeHeaderValue(offset, null);
        using var response = await client.SendAsync(request, HttpCompletionOption.ResponseHeadersRead, cancel);
        if (response.StatusCode == HttpStatusCode.PartialContent)
        {
            var range = response.Content.Headers.ContentRange;
            if (range is null || range.Unit != "bytes" || range.From != offset || range.To != size - 1 || range.Length != size) throw new InvalidDataException("下载来源返回了错误的断点范围。");
        }
        else if (response.StatusCode == HttpStatusCode.OK) offset = 0;
        else throw new InvalidOperationException("更新下载请求失败，请稍后重试。");
        if (response.Content.Headers.ContentLength is { } length && length != size - offset) throw new InvalidDataException("下载长度与签名清单不一致。");
        await using (var input = await response.Content.ReadAsStreamAsync(cancel))
        await using (var output = new FileStream(partial, offset == 0 ? FileMode.Create : FileMode.Append, FileAccess.Write, FileShare.None, 65536, true))
        {
            var buffer = new byte[65536]; long total = offset; int read;
            while ((read = await input.ReadAsync(buffer, cancel)) != 0)
            {
                total += read; if (total > size) throw new InvalidDataException("更新负载超过签名大小。");
                await output.WriteAsync(buffer.AsMemory(0, read), cancel); progress?.Invoke(total);
            }
            await output.FlushAsync(cancel); output.Flush(true);
            if (total != size) throw new IOException("下载中断，可继续下载。");
        }
        if (!await SafeFiles.Matches(partial, size, hash, cancel)) { File.Delete(partial); throw new InvalidDataException("下载摘要不匹配，已丢弃损坏负载。"); }
    }
    public static async Task PrepareFile(HttpClient client, ContentRelease release, ContentFile file, string cache, string stage, Action<long>? progress, CancellationToken cancel, LauncherBundleDownloader? delivery = null)
    {
        var hash = file.Compression == "gzip" ? file.BlobSha256 : file.Sha256;
        var partial = Path.Combine(cache, hash.ToLowerInvariant() + ".part");
        if (delivery is null) await Download(client, new Uri(release.Endpoint.TrimEnd('/') + "/blobs/" + hash), TransferSize(file), hash, partial, progress, cancel);
        else await delivery.DownloadContent(release, file, partial, progress, cancel);
        SafeFiles.NoLinks(stage);
        try
        {
            await using var input = File.OpenRead(partial);
            await using var output = new FileStream(stage, FileMode.Create, FileAccess.Write, FileShare.None, 65536, true);
            using var decompressor = file.Compression == "gzip" ? new GZipStream(input, CompressionMode.Decompress, true) : null;
            Stream plain = decompressor is null ? input : decompressor;
            var buffer = new byte[65536]; long total = 0; int read;
            while ((read = await plain.ReadAsync(buffer, cancel)) != 0)
            {
                total += read; if (total > file.Length) throw new InvalidDataException("解压负载超过签名大小。");
                await output.WriteAsync(buffer.AsMemory(0, read), cancel);
            }
            await output.FlushAsync(cancel); output.Flush(true);
            if (total != file.Length) throw new InvalidDataException("解压后文件长度不匹配。");
        }
        catch { File.Delete(stage); throw; }
        if (!await SafeFiles.Matches(stage, file.Length, file.Sha256, cancel)) { File.Delete(stage); throw new InvalidDataException("解压后文件摘要不匹配。"); }
    }
}
