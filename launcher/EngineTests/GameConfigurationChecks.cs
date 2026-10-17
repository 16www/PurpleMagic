using Dnf90.Launcher;
using ServerS5A3;

internal static class GameConfigurationChecks
{
    internal static async Task Run(string root, PlayerProfile profile, Action<bool, string> check,
        Func<Func<Task>, string, Task> reject)
    {
        var folder = Path.Combine(root, "game-config"); Directory.CreateDirectory(folder);
        var path = Path.Combine(folder, "DNF.cfg");
        var original = new byte[3 * 1024 * 1024 + 1]; Random.Shared.NextBytes(original);
        File.WriteAllBytes(path, original);
        var unrelated = Path.Combine(folder, "other.cfg"); File.WriteAllText(unrelated, "retain");
        Task Reset(bool confirmed, Action? guard = null) => Task.FromResult(GameConfiguration.ResetFile(path, confirmed, guard ?? (() => { })));
        await reject(() => Reset(false), "unconfirmed reset rejected before file mutation");
        check(File.ReadAllBytes(path).SequenceEqual(original) && Directory.GetFiles(folder, "*.bak").Length == 0, "unconfirmed reset preserves oversized config");
        await reject(() => Reset(true, () => throw new InvalidOperationException("game active")), "running game blocks reset");
        var calls = 0;
        await reject(() => Reset(true, () => { if (++calls == 2) throw new InvalidOperationException("game just started"); }), "recheck game immediately before rename");
        check(File.ReadAllBytes(path).SequenceEqual(original) && Directory.GetFiles(folder, "*.bak").Length == 0, "game-start refusal preserves original and creates no backup");
        if (OperatingSystem.IsWindows())
        {
            using var locked = new FileStream(path, FileMode.Open, FileAccess.ReadWrite, FileShare.None);
            await reject(() => Reset(true), "locked configuration cannot be reset");
            check(Directory.GetFiles(folder, "*.bak").Length == 0, "locked-file refusal leaves no backup or truncation");
        }
        var result = GameConfiguration.ResetFile(path, true, () => { });
        check(result.Reset && result.BackupPath is not null && !File.Exists(path), "oversized unknown-format config reset without parsing");
        check(File.ReadAllBytes(result.BackupPath!).SequenceEqual(original), "backup preserves every original byte beyond old 256 KiB limit");
        check(File.ReadAllText(unrelated) == "retain", "reset touches only the selected config fixture");
        var absent = GameConfiguration.ResetFile(path, true, () => { });
        check(!absent.Reset && absent.BackupPath is null && Directory.GetFiles(folder, "*.bak").Length == 1, "missing configuration is a no-op, does not invent a file");
        File.WriteAllText(path, "later settings");
        var again = GameConfiguration.ResetFile(path, true, () => { });
        check(again.BackupPath != result.BackupPath && File.ReadAllBytes(result.BackupPath!).SequenceEqual(original) && File.ReadAllText(again.BackupPath!) == "later settings", "repeated reset retains both independent backups");
        Directory.CreateDirectory(path);
        await reject(() => Reset(true), "directory masquerading as config rejected");
        Directory.Delete(path);
        using var engine = new LauncherEngine(profile, Path.Combine(root, "reset-state"));
        var preferences = new Preferences("fixture-client", "remembered-user", true, false, true);
        await engine.SavePreferences(preferences);
        var preferencesFile = Path.Combine(root, "reset-state", "preferences.json");
        var saved = File.ReadAllBytes(preferencesFile);
        var invoked = false;
        await reject(() => engine.ResetGameConfiguration(false, () => { invoked = true; return new(false, null); }), "engine also requires explicit confirmation");
        check(!invoked, "unconfirmed engine request cannot reach filesystem reset");
        var completed = await engine.ResetGameConfiguration(true, () =>
        {
            check(engine.State.Phase == "resetting" && !engine.State.CanLogin, "reset emits busy state and disables login");
            try { engine.SavePreferences(preferences).GetAwaiter().GetResult(); throw new Exception("lock bypassed"); }
            catch (InvalidOperationException) { check(true, "reset shares update/login/settings operation lock"); }
            return result;
        });
        check(completed == result && engine.State.Phase == "ready", "backup receipt reaches caller and reset finishes ready");
        check(File.ReadAllBytes(preferencesFile).SequenceEqual(saved) && engine.State.Preferences == preferences, "launcher account and settings are retained exactly");
        await reject(() => engine.ResetGameConfiguration(true, () => throw new IOException("fixture rename refused")), "reset failure reaches caller instead of success");
        check(engine.State.Phase == "error", "reset failure has error state");
        await engine.SavePreferences(preferences);
        check(engine.State.Phase == "ready", "failure releases operation lock for retry");
    }
}
