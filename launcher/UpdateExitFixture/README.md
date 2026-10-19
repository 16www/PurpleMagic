This headless native fixture exercises the Windows exit interval in which
`GetExitCodeProcess` returns 0 and .NET `HasExited` returns true, while
`DLL_PROCESS_DETACH` is still running and the process handle is not signaled.
It creates no windows and is never included in the player bundle.

Build with Windows C++ tools, then run the .NET 8 tests from the repository root:

```text
cmake -S launcher/UpdateExitFixture -B runtime/exit-fixture -A x64
cmake --build runtime/exit-fixture --config Release
dotnet run --project launcher/EngineTests -c Release -- --bundle-exit runtime/exit-fixture/Release
```

The tests check owner waiting, an unlisted launcher in the same installation,
rollback waiting, creation-time identity, and refusal to replace a process
still detaching. They verify the directory can be renamed after the kernel
signals completion. Existing `--bundle-lifecycle` tests separately cover
forced closure, other installations/game processes, locks, and health rollback.
