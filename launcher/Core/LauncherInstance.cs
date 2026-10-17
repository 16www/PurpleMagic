namespace ServerS5A3;

public static class LauncherInstance
{
    // A released mutex can still exist because an update helper holds a handle.
    // Ownership, rather than the constructor's createdNew flag, excludes a peer.
    public static bool TryAcquire(Mutex mutex, int millisecondsTimeout = 0)
    {
        try { return mutex.WaitOne(millisecondsTimeout); }
        catch (AbandonedMutexException) { return true; } // WaitOne granted ownership.
    }
}
