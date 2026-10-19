// Test only: keep the process in DLL teardown after Windows sets exit code 0.
#include <windows.h>
__declspec(dllexport) void keep_library(void) {}
BOOL WINAPI DllMain(HINSTANCE dll, DWORD reason, LPVOID reserved) {
    if (reason == DLL_PROCESS_DETACH && reserved) {
        HANDLE ready = CreateFileW(L"detach.ready", GENERIC_WRITE,
            FILE_SHARE_READ | FILE_SHARE_WRITE | FILE_SHARE_DELETE,
            NULL, CREATE_ALWAYS, FILE_ATTRIBUTE_NORMAL, NULL);
        if (ready != INVALID_HANDLE_VALUE) CloseHandle(ready);
        Sleep(1500);
    }
    return TRUE;
}
