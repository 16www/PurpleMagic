// Headless native lifecycle fixture. No console/window, even on helper fallback.
#include <windows.h>
__declspec(dllimport) void keep_library(void);
int WINAPI wWinMain(HINSTANCE instance, HINSTANCE previous, PWSTR command, int show) {
    keep_library(); Sleep(200); return 0;
}
