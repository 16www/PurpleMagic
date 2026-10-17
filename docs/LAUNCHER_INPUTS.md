# Launcher build inputs

The source uses Flutter Windows, .NET 8 NativeAOT and Visual Studio C++ tools.
Use the installed tools through explicit build arguments; build scripts do not
install SDKs. Preserve the approved profile's signing public key, certificate,
EXE/PVF hashes, account endpoint and channel identity.

Private input locations:

- `runtime/launcher-input/profile.json`, or `--profile-dir <protected-directory>`.
- `launcher/Flutter/assets/background.webp`.
- `launcher/Flutter/assets/announcement.webp`.
- `launcher/Flutter/assets/dnf-logo.png`.
- `launcher/Flutter/windows/runner/resources/app_icon.ico`.

These paths are ignored. Supply the existing approved files for a real build.
`launcher/profile.example.json` documents the profile fields; its placeholders
are not valid client compatibility evidence.

Example build command after supplying inputs:

```text
BUILD_FLUTTER.bat --flutter <flutter-sdk> --dotnet <dotnet-executable> --profile-dir <protected-directory> --output dist/launcher-candidate
```

The output must be a new directory. BUILD_PORTABLE.bat takes `--bundle` pointing
to the complete ZIP and an explicit new `--output`. Builds do not publish updates
or replace installed player copies.
