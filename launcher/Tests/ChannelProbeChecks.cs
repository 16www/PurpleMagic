using System.Buffers.Binary;
using System.IO.Compression;
using System.Security.Cryptography;
using System.Text;
using ServerS5A3;

internal static class ChannelProbeChecks
{
    internal static async Task Run(Action<bool, string> check, Action<Action, string> reject)
    {
        var body = new byte[112];
        BinaryPrimitives.WriteInt32LittleEndian(body, 1);
        BinaryPrimitives.WriteInt32LittleEndian(body.AsSpan(8), 2);
        for (var i = 0; i < 2; i++)
        {
            Encoding.ASCII.GetBytes("192.0.2.1").CopyTo(body, 12 + i * 48 + 28);
            BinaryPrimitives.WriteInt32LittleEndian(body.AsSpan(12 + i * 48 + 44), 10010 + i);
        }
        check(ChannelProbe.DirectoryPorts(body, "192.0.2.1").SequenceEqual(new[] { 10010, 10011 }), "real directory row offsets and zero padding");
        var migrated = (byte[])body.Clone();
        BinaryPrimitives.WriteInt32LittleEndian(migrated.AsSpan(4), 1);
        reject(() => ChannelProbe.DirectoryPorts(migrated, "192.0.2.1"), "old default cannot silently accept foreign directory");
        check(ChannelProbe.DirectoryPorts(migrated, "192.0.2.1", 1).SequenceEqual(new[] {10010, 10011}), "configured directory 1 accepted");
        check(ChannelProbe.DirectoryPorts(body, "192.0.2.1", 1, 0).Length == 2, "staged launcher works before server migration");
        check(ChannelProbe.DirectoryPorts(migrated, "192.0.2.1", 1, 0).Length == 2, "staged launcher works after server migration");
        reject(() => ChannelProbe.DirectoryPorts(body, "192.0.2.1", 1), "removing previous directory closes migration window");
        foreach (var foreign in new[] {-1, 2, 249, 250, 256, int.MaxValue}) {
            var packet = (byte[])body.Clone(); BinaryPrimitives.WriteInt32LittleEndian(packet.AsSpan(4), foreign);
            reject(() => ChannelProbe.DirectoryPorts(packet, "192.0.2.1", 1, 0), "migration rejects unrelated directory " + foreign);
        }
        foreach (var invalid in new[] {-1, 250, 256}) {
            reject(() => ChannelProbe.DirectoryPorts(body, "192.0.2.1", invalid, 0), "invalid preferred directory rejected");
            reject(() => ChannelProbe.DirectoryPorts(body, "192.0.2.1", 0, invalid), "invalid migration directory rejected");
        }
        reject(() => ChannelProbe.DirectoryPorts(body, "192.0.2.1", 0, 0), "redundant migration configuration rejected");
        reject(() => ChannelProbe.DirectoryPorts(body, "192.0.2.2"), "directory cannot redirect diagnostic connections to another host");
        var bad = (byte[])body.Clone(); BinaryPrimitives.WriteInt32LittleEndian(bad.AsSpan(56), 22);
        reject(() => ChannelProbe.DirectoryPorts(bad, "192.0.2.1"), "directory cannot redirect probes to management ports");
        reject(() => ChannelProbe.DirectoryPorts(body[..80], "192.0.2.1"), "truncated directory rejected");
        bad = (byte[])body.Clone(); BinaryPrimitives.WriteInt32LittleEndian(bad.AsSpan(8), int.MaxValue);
        reject(() => ChannelProbe.DirectoryPorts(bad, "192.0.2.1"), "hostile count rejected without overflow");
        bad = (byte[])body.Clone(); BinaryPrimitives.WriteInt32LittleEndian(bad.AsSpan(8), 0);
        reject(() => ChannelProbe.DirectoryPorts(bad, "192.0.2.1"), "empty directory not called healthy");
        var key = RandomNumberGenerator.GetBytes(16);
        using var aes = Aes.Create(); aes.Key = key;
        byte[] Compress(byte[] input) {
            using var output = new MemoryStream();
            using (var zlib = new ZLibStream(output, CompressionLevel.Optimal, true)) zlib.Write(input);
            return output.ToArray();
        }
        var compressed = Compress(aes.EncryptEcb(body, PaddingMode.None));
        check(ChannelProbe.Decode(compressed, key).SequenceEqual(body), "channel zlib and AES decoding");
        reject(() => ChannelProbe.Decode(Compress(new byte[512 * 1024 + 16]), key), "compressed expansion bounded");
        var frame = ChannelProbe.Request(0x12, compressed.Length + 11); compressed.CopyTo(frame, 11);
        using (var stream = new FragmentedStream(frame))
            check((await ChannelProbe.ReadPacket(stream, 0x12, CancellationToken.None)).SequenceEqual(frame), "fragmented TCP reads reassembled");
        async Task Rejected(byte[] bytes, CancellationToken cancel, string name) {
            using var stream = new FragmentedStream(bytes);
            var rejected = false;
            try { await ChannelProbe.ReadPacket(stream, 0x12, cancel); }
            catch (Exception error) when (error is IOException or InvalidDataException or OperationCanceledException) { rejected = true; }
            check(rejected, name);
        }
        await Rejected(frame[..^1], CancellationToken.None, "EOF during payload rejected");
        await Rejected(ChannelProbe.Request(0x0c, 47), CancellationToken.None, "wrong response operation rejected");
        var oversized = ChannelProbe.Request(0x12, 11);
        BinaryPrimitives.WriteInt32LittleEndian(oversized.AsSpan(2), int.MaxValue);
        await Rejected(oversized, CancellationToken.None, "oversized frame rejected before allocation");
        await Rejected(frame, new CancellationToken(true), "cancelled exchange exits promptly");
    }

    private sealed class FragmentedStream(byte[] data) : MemoryStream(data)
    {
        public override ValueTask<int> ReadAsync(Memory<byte> buffer, CancellationToken cancellationToken = default) =>
            base.ReadAsync(buffer[..Math.Min(3, buffer.Length)], cancellationToken);
    }
}
