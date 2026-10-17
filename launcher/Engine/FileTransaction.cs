using System.Text.Json;

namespace Dnf90.Launcher;

// The same journal owns managed files and the installed metadata receipt. Recovery
// before the commit marker restores both. Unmanaged files are never enumerated/deleted.
public sealed class FileTransaction : IDisposable
{
    private readonly string root, work, journalPath, receipt;
    private readonly FileStream gate;
    public Action<string, int>? FaultPoint { get; set; }
    public FileTransaction(string directory)
    {
        root = Path.GetFullPath(directory); SafeFiles.NoLinks(root);
        work = Path.Combine(root, ".dnf90-update"); SafeFiles.NoLinks(work); Directory.CreateDirectory(work);
        gate = new FileStream(Path.Combine(work, "update.lock"), FileMode.OpenOrCreate, FileAccess.ReadWrite, FileShare.None);
        journalPath = Path.Combine(work, "journal.json"); receipt = Path.Combine(work, "installed.json");
    }
    public string Stage(int index) => Path.Combine(work, "stage-" + index);
    private string Backup(int index) => Path.Combine(work, "backup-" + index);
    private void Save(TransactionJournal j) => SafeFiles.Atomic(journalPath, JsonSerializer.SerializeToUtf8Bytes(j, WireJson.Default.TransactionJournal));
    public void Recover()
    {
        if (!File.Exists(journalPath)) return;
        SafeFiles.NoLinks(journalPath);
        if (new FileInfo(journalPath).Length > Trust.MaxMetadata) throw new InvalidDataException("更新恢复记录无效。");
        var j = JsonSerializer.Deserialize(File.ReadAllBytes(journalPath), WireJson.Default.TransactionJournal) ?? throw new InvalidDataException("更新恢复记录无效。");
        if (j.Entries.Length > 2000 || j.Phase is not ("prepared" or "committed")) throw new InvalidDataException("更新恢复阶段无效。");
        foreach (var item in j.Entries) _ = SafeFiles.Target(root, item.Path);
        if (j.Phase == "prepared")
        {
            for (int i = j.Entries.Length - 1; i >= 0; i--)
            {
                var item = j.Entries[i]; var target = SafeFiles.Target(root, item.Path); var backup = Backup(i); SafeFiles.NoLinks(backup); SafeFiles.NoLinks(Stage(i));
                if (File.Exists(backup)) { File.Move(backup, target, true); }
                else if (!item.HadOriginal && !File.Exists(Stage(i))) { File.Delete(target); }
            }
            if (j.PreviousReceipt is null) File.Delete(receipt); else SafeFiles.Atomic(receipt, Convert.FromBase64String(j.PreviousReceipt));
        }
        for (int i = 0; i < j.Entries.Length; i++) { SafeFiles.NoLinks(Stage(i)); SafeFiles.NoLinks(Backup(i)); File.Delete(Stage(i)); File.Delete(Backup(i)); }
        File.Delete(journalPath);
    }
    public void Commit(string[] paths, byte[] installed)
    {
        if (File.Exists(journalPath)) throw new InvalidOperationException("需要先恢复之前的更新。");
        var entries = paths.Select(p => new TransactionEntry(p, File.Exists(SafeFiles.Target(root, p)))).ToArray();
        var j = new TransactionJournal("prepared", entries, File.Exists(receipt) ? Convert.ToBase64String(File.ReadAllBytes(receipt)) : null);
        for (int i = 0; i < entries.Length; i++) { SafeFiles.NoLinks(Stage(i)); if (!File.Exists(Stage(i)) || File.Exists(Backup(i))) throw new InvalidDataException("暂存或备份状态无效。"); }
        Save(j); FaultPoint?.Invoke("prepared", -1);
        try
        {
            for (int i = 0; i < entries.Length; i++)
            {
                var target = SafeFiles.Target(root, entries[i].Path); Directory.CreateDirectory(Path.GetDirectoryName(target)!);
                if (entries[i].HadOriginal) File.Move(target, Backup(i)); FaultPoint?.Invoke("backed-up", i);
                File.Move(Stage(i), target); FaultPoint?.Invoke("installed", i);
            }
            SafeFiles.Atomic(receipt, installed); FaultPoint?.Invoke("receipt", -1);
            Save(j with { Phase = "committed" }); FaultPoint?.Invoke("committed", -1);
        }
        catch (SimulatedPowerLoss) { throw; }
        catch { Recover(); throw; }
        Recover();
    }
    public void Dispose() => gate.Dispose();
}
// Fault injection only; no production path constructs this exception.
public sealed class SimulatedPowerLoss : Exception { }
