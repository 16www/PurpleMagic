using System.IO.Compression;
using System.Net;
using System.Net.Http.Headers;
using Dnf90.Launcher;
using ServerS5A3;

internal static class ContentDirectDownloadChecks
{
    internal static async Task Run(string temp, PlayerProfile root, Action<bool,string> check)
    {
        byte[] plain = "final guild EXE fixture, same signed bytes"u8.ToArray();
        using var compressed = new MemoryStream();
        using (var gz = new GZipStream(compressed, CompressionMode.Compress, true)) gz.Write(plain);
        var blob = compressed.ToArray(); var hash = Trust.Digest(blob);
        var file = new ContentFile("DNF.exe",plain.Length,Trust.Digest(plain),true,"gzip",hash,blob.Length);
        var release = new ContentRelease("v107","local-guild","stable",2,new string('d',64),root.ApiUrl,root.CertificateSha256,[file]);
        var cache = Path.Combine(temp,"direct-content");Directory.CreateDirectory(cache);
        var partial = Path.Combine(cache,hash+".part");File.WriteAllBytes(partial,blob[..5]);
        int origins=0,providers=0;
        using var transport = new LauncherBundleDownloader(root,new Handler(r=>
        {
            check(r.Headers.Range?.Ranges.Single().From==5,"content resumes existing old-client partial through all hops");
            if(r.RequestUri!.Host==new Uri(root.ApiUrl).Host)
            {
                origins++; check(r.Headers.GetValues(LauncherBundleDownloader.CapabilityHeader).Single()=="baidu-v1","content explicitly negotiates direct delivery");
                var redirect = new HttpResponseMessage(HttpStatusCode.TemporaryRedirect);redirect.Headers.Location=new Uri("https://cdn.baidupcs.com/opaque?expires=fixture");return redirect;
            }
            providers++;check(r.Headers.UserAgent.ToString()=="pan.baidu.com" && !r.Headers.Contains(LauncherBundleDownloader.CapabilityHeader) && r.Headers.Authorization is null,"provider receives UA and range, no gateway authorization");
            var response=new HttpResponseMessage(HttpStatusCode.PartialContent){Content=new ByteArrayContent(blob[5..])};response.Content.Headers.ContentRange=new ContentRangeHeaderValue(5,blob.Length-1,blob.Length);return response;
        }));
        using var legacy = new HttpClient(new Handler(_=>throw new Exception("legacy client unexpectedly used")));
        var stage=Path.Combine(temp,"direct-stage.exe");
        await ContentUpdater.PrepareFile(legacy,release,file,cache,stage,null,default,transport);
        check(File.ReadAllBytes(stage).SequenceEqual(plain) && origins==1 && providers==1,"direct download validates compressed and final EXE hashes");
        await ContentUpdater.PrepareFile(legacy,release,file,cache,stage,null,default,transport);
        check(origins==1 && providers==1,"verified content cache causes zero second download");
        var client=Path.Combine(temp,"direct-installed");Directory.CreateDirectory(client);File.Copy(stage,Path.Combine(client,file.Path));
        check((await ContentUpdater.Plan(client,release,default)).Length==0,"final installed EXE produces zero update plan");
        File.Delete(partial);int attempts=0;
        using var fallback=new LauncherBundleDownloader(root,new Handler(r=>
        {
            attempts++;
            if(attempts<3){check((attempts==1)==!r.Headers.Contains("X-DNF90-Refresh-Link"),"expired link retry requests fresh resolution");return new(HttpStatusCode.Forbidden);}
            check(!r.Headers.Contains(LauncherBundleDownloader.CapabilityHeader),"content fallback retains old byte protocol");return new(HttpStatusCode.OK){Content=new ByteArrayContent(blob)};
        }));
        await fallback.DownloadContent(release,file,partial,null,default);
        check(attempts==3 && File.ReadAllBytes(partial).SequenceEqual(blob),"expired provider falls back with identical hash");
        File.Delete(partial);
        using var bad=new LauncherBundleDownloader(root,new Handler(_=>new(HttpStatusCode.OK){Content=new ByteArrayContent(new byte[blob.Length])}));
        try {await bad.DownloadContent(release,file,partial,null,default);throw new Exception("bad hash accepted");}
        catch(InvalidDataException){check(!File.Exists(partial),"content integrity failure removes corrupt bytes without fallback");}
        try {await bad.DownloadContent(release with {Endpoint="https://evil.example"},file,partial,null,default);throw new Exception("foreign content accepted");}
        catch(InvalidDataException){check(true,"foreign content origin rejected");}

        var unicode=Path.Combine(temp,"玩家客户端 中文路径","客户端");Directory.CreateDirectory(Path.Combine(unicode,"ImagePacks2"));
        byte[] npk="unchanged NPK bytes"u8.ToArray();byte[] pvf="new PVF bytes"u8.ToArray();
        var kept=new ContentFile("ImagePacks2/Sprite_ITEM.NPK",npk.Length,Trust.Digest(npk).ToUpperInvariant(),true,"none","",0);
        var changedPvf=new ContentFile("Script.pvf",pvf.Length,Trust.Digest(pvf),true,"none","",0);
        File.WriteAllBytes(Path.Combine(unicode,kept.Path),npk);
        File.WriteAllText(Path.Combine(unicode,"DNF.exe"),"old exe");File.WriteAllText(Path.Combine(unicode,"Script.pvf"),"old pvf");
        var pair=release with {Files=[file,changedPvf,kept]};
        var plan=await ContentUpdater.Plan(unicode,pair,default);
        check(plan.Select(f=>f.Path).SequenceEqual(new[]{"DNF.exe","Script.pvf"}),"Chinese paths and uppercase hashes keep unchanged NPK out of download plan");
        File.WriteAllBytes(Path.Combine(unicode,"DNF.exe"),plain);File.WriteAllBytes(Path.Combine(unicode,"Script.pvf"),pvf);
        check((await ContentUpdater.Plan(unicode,pair,default)).Length==0,"second Chinese-path plan downloads nothing");
    }
    private sealed class Handler(Func<HttpRequestMessage,HttpResponseMessage> f):HttpMessageHandler
    { protected override Task<HttpResponseMessage> SendAsync(HttpRequestMessage r,CancellationToken c)=>Task.FromResult(f(r)); }
}
