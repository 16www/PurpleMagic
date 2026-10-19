export type Envelope<T> = {
  data: T;
  page?: { limit: number; nextCursor?: string };
  requestId?: string;
  error?: { code: string; message: string; retryable: boolean };
};
export class ApiError extends Error {
  constructor(
    message: string,
    public status = 0,
    public code = "network_error",
    public requestId = "",
  ) {
    super(message);
  }
}
export class HttpClient {
  csrf = "";
  hubCsrf = "";
  constructor(private fetcher: typeof fetch = (...args) => fetch(...args)) {}
  async raw<T = any>(
    path: string,
    method = "GET",
    body?: unknown,
    key?: string,
    signal?: AbortSignal,
    headers: Record<string, string> = {},
  ): Promise<T> {
    if (!path.startsWith("/api/") && !path.startsWith("/admin/api/"))
      throw new Error("接口路径无效");
    let response: Response;
    try {
      response = await this.fetcher(path, {
        method,
        credentials: "same-origin",
        cache: "no-store",
        signal,
        headers: {
          Accept: "application/json",
          ...(method !== "GET"
            ? {
                "Content-Type": "application/json",
                "X-CSRF-Token": path.startsWith("/admin/")
                  ? this.hubCsrf
                  : this.csrf,
              }
            : {}),
          ...(key ? { "Idempotency-Key": key } : {}),
          ...headers,
        },
        body: body === undefined ? undefined : JSON.stringify(body),
      });
    } catch (e) {
      if ((e as Error).name === "AbortError") throw e;
      throw new ApiError("连接中断，可使用原请求重试并查询回执");
    }
    let value: any;
    try {
      value = await response.json();
    } catch (e) {
      if ((e as Error).name === "AbortError") throw e;
      throw new ApiError(
        "接口未返回有效数据",
        response.status,
        "invalid_response",
      );
    }
    if (
      !response.ok ||
      value.error ||
      value.success === false ||
      value.ok === false
    ) {
      throw new ApiError(
        typeof value.error === "string"
          ? value.error
          : value.error?.message || value.message || "请求未完成",
        response.status,
        value.error?.code || "request_failed",
        value.requestId || "",
      );
    }
    return value;
  }
  async get<T = any>(path: string, signal?: AbortSignal) {
    return this.raw<Envelope<T>>(path, "GET", undefined, undefined, signal);
  }
  async post<T = any>(path: string, body: unknown, key?: string) {
    return (await this.raw<Envelope<T>>(path, "POST", body, key)).data;
  }
  async capabilities() {
    const { data } = await this.get<{
      features: Record<string, boolean>;
      csrfToken: string;
      backgroundMail?: boolean;
    }>("/api/v1/capabilities");
    this.csrf = data.csrfToken;
    return data;
  }
  async login(key: string, remember = true) {
    await this.raw("/api/auth/login", "POST", { password: key, remember });
    return this.capabilities();
  }
}
export function query(
  path: string,
  params: Record<string, string | number | undefined>,
) {
  const q = new URLSearchParams();
  for (const [k, v] of Object.entries(params))
    if (v !== undefined && v !== "") q.set(k, String(v));
  return path + "?" + q;
}
export const segment = encodeURIComponent;
export function safeNumber(value: unknown): number {
  const n = Number(value);
  if (value == null || !Number.isSafeInteger(n)) return Number.NaN;
  return n;
}
