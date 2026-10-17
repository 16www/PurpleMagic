using ServerS5A3;

namespace Dnf90.Launcher;

public static class GameConfiguration
{
    public static ConfigurationReset Reset(bool confirmed)
    {
        if (!confirmed) throw new InvalidOperationException("请先确认重置本机游戏配置。");
        return ResetFile(NativeChannelHistory.ConfigurationPath(), confirmed, LauncherEngine.RequireNoGames);
    }

    // Only isolated tests supply a path. The pipe API always resolves the current
    // Windows user's LocalLow folder; client input cannot select a reset target.
    internal static ConfigurationReset ResetFile(string path, bool confirmed, Action requireNoGames)
    {
        if (!confirmed) throw new InvalidOperationException("请先确认重置本机游戏配置。");
        requireNoGames();
        path = Path.GetFullPath(path);
        SafeFiles.NoLinks(path);
        FileAttributes attributes;
        try { attributes = File.GetAttributes(path); }
        catch (FileNotFoundException) { return new(false, null); }
        catch (DirectoryNotFoundException) { return new(false, null); }
        if ((attributes & FileAttributes.Directory) != 0) throw new IOException("游戏配置路径不是文件。");
        var backup = path + ".DNF90-reset-" + DateTime.UtcNow.ToString("yyyyMMddHHmmss") + "-" + Guid.NewGuid().ToString("N") + ".bak";
        SafeFiles.NoLinks(backup);
        // Hold a handle that denies writers while allowing the same-volume rename.
        // No decoding, size cap, truncation or delete: even a large/unknown-format
        // file is preserved intact, and an existing backup can never be replaced.
        using var original = new FileStream(path, FileMode.Open, FileAccess.Read, FileShare.Read | FileShare.Delete);
        requireNoGames();
        SafeFiles.NoLinks(path);
        File.Move(path, backup, overwrite: false);
        return new(true, backup);
    }
}
