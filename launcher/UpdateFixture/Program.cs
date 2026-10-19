// Headless executable used only by isolated updater lifecycle tests.
// This project is never part of the player package.
if (args.Length == 2 && args[0] == "--hold-ready")
{
    File.WriteAllText(args[1], "ready");
    await Task.Delay(Timeout.Infinite);
    return 0;
}
if (args.Length == 2 && args[0] == "--update-health")
{
    if (File.Exists(Path.Combine(AppContext.BaseDirectory, "unhealthy"))) return 3;
    File.WriteAllText(args[1], "ready");
    return 0;
}
File.WriteAllText(Path.Combine(AppContext.BaseDirectory, "fixture-started"), string.Join(" ", args));
return 0;
