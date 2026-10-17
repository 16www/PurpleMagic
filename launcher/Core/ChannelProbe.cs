using System.Buffers.Binary;
using System.IO.Compression;
using System.Net;
using System.Net.Sockets;
using System.Security.Cryptography;
using System.Text;

namespace ServerS5A3;

public sealed record ChannelProbeResult(int Channels, int TestedPorts, int[] ChannelIds)
{
    public int ServerId { get; init; }
    public string Message => $"连接检测通过：频道列表 {Channels} 项，{TestedPorts} 个游戏入口可达。";
}

// A read-only native bootstrap exchange. No credentials, account admission,
// game login or game process are needed. Only the configured server is probed.
public static class ChannelProbe
{
    private const int MaxPacket = 128 * 1024;
    private const int MaxPlain = 512 * 1024;

    public static void ValidateConfiguration(PlayerProfile profile) =>
        ValidateDirectoryServer(profile.ChannelServerId, profile.ChannelServerId, profile.PreviousChannelServerId);

    public static async Task<ChannelProbeResult> Check(PlayerProfile profile, Action<string>? progress = null)
    {
        ValidateConfiguration(profile);
        var stage = "C01";
        try
        {
            progress?.Invoke("正在检测频道入口…");
            using var connection = new TcpClient { NoDelay = true };
            using (var timeout = new CancellationTokenSource(TimeSpan.FromSeconds(6)))
                await connection.ConnectAsync(profile.GameHost, profile.BootstrapPort, timeout.Token);
            await using var stream = connection.GetStream();
            stage = "C02";
            using var exchange = new CancellationTokenSource(TimeSpan.FromSeconds(12));
            var cancel = exchange.Token;
            await stream.WriteAsync(Request(0x0b, 43), cancel);
            var ack = await ReadPacket(stream, 0x0c, cancel);
            if (ack.Length != 47 || BinaryPrimitives.ReadInt32LittleEndian(ack.AsSpan(11)) != 0)
                throw new InvalidDataException("Invalid bootstrap acknowledgement");
            var key = ack[15..31];
            stage = "C03";
            progress?.Invoke("正在检测频道表下载…");
            await stream.WriteAsync(Request(0x09, 11), cancel);
            var script = Decode((await ReadPacket(stream, 0x0a, cancel))[11..], key);
            if (!Encoding.ASCII.GetString(script).Contains("[server]", StringComparison.Ordinal))
                throw new InvalidDataException("Missing channel script");
            stage = "C04";
            var request = Request(0x11, 35);
            "cain"u8.CopyTo(request.AsSpan(11));
            await stream.WriteAsync(request, cancel);
            var directory = Decode((await ReadPacket(stream, 0x12, cancel))[11..], key);
            var ports = DirectoryPorts(directory, profile.GameHost, profile.ChannelServerId, profile.PreviousChannelServerId);
            var serverId = BinaryPrimitives.ReadInt32LittleEndian(directory.AsSpan(4));
            var count = BinaryPrimitives.ReadInt32LittleEndian(directory.AsSpan(8));
            stage = "G01";
            progress?.Invoke("正在检测游戏入口…");
            // A small representative sample including the ordinary landing port.
            // This distinguishes a working bootstrap from blocked game traffic.
            var selected = ports.OrderBy(port => port == 10010 ? 0 : 1).ThenBy(port => port).Take(3).ToArray();
            await Task.WhenAll(selected.Select(async port => {
                using var game = new TcpClient();
                using var timeout = new CancellationTokenSource(TimeSpan.FromSeconds(6));
                await game.ConnectAsync(profile.GameHost, port, timeout.Token);
            }));
            return new ChannelProbeResult(count, selected.Length, ports.Select(port => port - 10000).ToArray()) { ServerId = serverId };
        }
        catch (Exception error) when (error is IOException or InvalidDataException or SocketException or OperationCanceledException or CryptographicException)
        {
            var description = stage switch {
                "C01" => "无法连接频道入口",
                "C02" => "频道握手失败",
                "C03" => "频道表下载或解码失败",
                "C04" => "频道列表接收或校验失败",
                _ => "频道入口可达，但游戏端口连接失败"
            };
            // Stable support codes only: never expose IPs, raw packets or keys.
            throw new InvalidOperationException($"连接检测 {stage}：{description}。请将此提示反馈给管理员。", error);
        }
    }

    internal static byte[] Request(byte operation, int size)
    {
        var raw = new byte[size]; raw[1] = operation; raw[10] = 1;
        BinaryPrimitives.WriteInt32LittleEndian(raw.AsSpan(2), size);
        return raw;
    }

    internal static async Task<byte[]> ReadPacket(Stream stream, byte operation, CancellationToken cancel)
    {
        var header = new byte[11];
        await stream.ReadExactlyAsync(header, cancel);
        var length = BinaryPrimitives.ReadInt32LittleEndian(header.AsSpan(2));
        if (length < 11 || length > MaxPacket || header[1] != operation || header[10] != 1)
            throw new InvalidDataException("Invalid channel frame");
        var packet = new byte[length]; header.CopyTo(packet, 0);
        await stream.ReadExactlyAsync(packet.AsMemory(11), cancel);
        return packet;
    }

    internal static byte[] Decode(byte[] compressed, byte[] key)
    {
        using var input = new MemoryStream(compressed);
        using var zlib = new ZLibStream(input, CompressionMode.Decompress);
        using var output = new MemoryStream();
        var buffer = new byte[4096]; int read;
        while ((read = zlib.Read(buffer)) != 0)
        {
            if (output.Length + read > MaxPlain) throw new InvalidDataException("Oversized channel payload");
            output.Write(buffer, 0, read);
        }
        if (output.Length == 0 || output.Length % 16 != 0) throw new InvalidDataException("Invalid encrypted payload");
        using var aes = Aes.Create(); aes.Key = key;
        return aes.DecryptEcb(output.ToArray(), PaddingMode.None);
    }

    internal static void ValidateDirectoryServer(int actual, int expected, int? previous)
    {
        if (expected is < 0 or >= 250 || previous is < 0 or >= 250 || previous == expected ||
            actual != expected && actual != previous)
            throw new InvalidDataException("Unexpected channel directory identity");
    }

    internal static int[] DirectoryPorts(byte[] data, string expectedHost, int expectedServer = 0, int? previousServer = null)
    {
        if (data.Length < 12 || BinaryPrimitives.ReadInt32LittleEndian(data) != 1)
            throw new InvalidDataException("Invalid directory header");
        ValidateDirectoryServer(BinaryPrimitives.ReadInt32LittleEndian(data.AsSpan(4)), expectedServer, previousServer);
        var count = BinaryPrimitives.ReadInt32LittleEndian(data.AsSpan(8));
        if (count <= 0 || count > 1024 || data.Length < 12 + count * 48 || data.Length - (12 + count * 48) >= 16)
            throw new InvalidDataException("Invalid directory size");
        var expected = IPAddress.Parse(expectedHost);
        var ports = new HashSet<int>();
        for (var i = 0; i < count; i++)
        {
            var row = data.AsSpan(12 + i * 48, 48);
            var host = Encoding.ASCII.GetString(row.Slice(28, 16)).TrimEnd('\0');
            var port = BinaryPrimitives.ReadInt32LittleEndian(row[44..]);
            if (!IPAddress.TryParse(host, out var actual) || !actual.Equals(expected) || port is < 10001 or > 10999)
                throw new InvalidDataException("Unexpected game destination");
            ports.Add(port);
        }
        return ports.ToArray();
    }
}
