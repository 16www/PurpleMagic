#include <flutter/dart_project.h>
#include <flutter/flutter_view_controller.h>
#include <windows.h>
#include <algorithm>
#include <memory>
#include <shlobj.h>

#include "flutter_window.h"
#include "utils.h"

int APIENTRY wWinMain(_In_ HINSTANCE instance, _In_opt_ HINSTANCE prev,
                      _In_ wchar_t *command_line, _In_ int show_command) {
  // The native UI must be single-instance too: a rejected second core used to
  // leave another Flutter process holding the same update directory open.
  const auto arguments = GetCommandLineArguments();
  const bool guard_check = arguments.size() == 1 && arguments[0] == "--verify-launch-guard";
  HANDLE raw_gate = CreateMutexW(nullptr, TRUE, L"Local\\DNF90Launcher.UI.v1");
  const bool duplicate = raw_gate && GetLastError() == ERROR_ALREADY_EXISTS;
  const auto closer = [](void* value) { if (value) CloseHandle(value); };
  std::unique_ptr<void, decltype(closer)> gate(raw_gate, closer);
  bool core_running = false;
  PWSTR local = nullptr;
  if (SUCCEEDED(SHGetKnownFolderPath(FOLDERID_LocalAppData, 0, nullptr, &local))) {
    const std::wstring path = std::wstring(local) + L"\\DNF90Launcher\\instance.lock";
    CoTaskMemFree(local);
    HANDLE file = CreateFileW(path.c_str(), GENERIC_READ | GENERIC_WRITE, 0,
                             nullptr, OPEN_EXISTING, FILE_ATTRIBUTE_NORMAL, nullptr);
    if (file == INVALID_HANDLE_VALUE) core_running = GetLastError() == ERROR_SHARING_VIOLATION;
    else CloseHandle(file);
  }
  if (!gate || duplicate || core_running) {
    if (!guard_check) {
      const auto existing = FindWindowW(nullptr, L"DNF 90CN");
      if (existing) { ShowWindow(existing, SW_RESTORE); SetForegroundWindow(existing); }
    }
    return 2;
  }
  if (guard_check) return 0; // Headless verification, before COM/Flutter/window creation.
  // Attach to console when present (e.g., 'flutter run') or create a
  // new console when running with a debugger.
  if (!::AttachConsole(ATTACH_PARENT_PROCESS) && ::IsDebuggerPresent()) {
    CreateAndAttachConsole();
  }

  // Initialize COM, so that it is available for use in the library and/or
  // plugins.
  ::CoInitializeEx(nullptr, COINIT_APARTMENTTHREADED);

  flutter::DartProject project(L"data");

  std::vector<std::string> command_line_arguments =
      GetCommandLineArguments();

  project.set_dart_entrypoint_arguments(std::move(command_line_arguments));

  FlutterWindow window(project);
  RECT work = {};
  SystemParametersInfo(SPI_GETWORKAREA, 0, &work, 0);
  const double scale = GetDpiForSystem() / 96.0;
  const auto available_width = static_cast<unsigned int>((work.right - work.left) / scale);
  const auto available_height = static_cast<unsigned int>((work.bottom - work.top) / scale);
  const auto width = std::min(1440u, available_width - 48u);
  const auto height = std::min(810u, available_height - 48u);
  Win32Window::Point origin((available_width - width) / 2u, (available_height - height) / 2u);
  Win32Window::Size size(width, height);
  if (!window.Create(L"DNF 90CN", origin, size)) {
    return EXIT_FAILURE;
  }
  window.SetQuitOnClose(true);

  ::MSG msg;
  while (::GetMessage(&msg, nullptr, 0, 0)) {
    ::TranslateMessage(&msg);
    ::DispatchMessage(&msg);
  }

  ::CoUninitialize();
  return EXIT_SUCCESS;
}
