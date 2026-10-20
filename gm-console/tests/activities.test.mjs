import test from "node:test";
import assert from "node:assert/strict";
import { build } from "esbuild";
import { initialState } from "../src/fixtures.ts";
import { applyCommand } from "../src/domain.ts";
import {
  activityFromDTO,
  changedActivities,
  filterActivities,
  pruneActivityDraft,
} from "../src/activity.ts";

const { outputFiles } = await build({
  entryPoints: ["src/api/console.ts"],
  bundle: true,
  platform: "node",
  format: "esm",
  write: false,
});
const { ConsoleAPI } = await import(
  "data:text/javascript;base64," +
    Buffer.from(outputFiles[0].text).toString("base64"),
);

const storage = new Map();
globalThis.sessionStorage = {
  getItem: (key) => storage.get(key) ?? null,
  setItem: (key, value) => storage.set(key, value),
  removeItem: (key) => storage.delete(key),
};

const envelope = (data, status = 200) =>
  new Response(
    JSON.stringify(
      status === 200
        ? { data }
        : { error: { code: "revision_conflict", message: "revision changed" } },
    ),
    { status },
  );

test("activity rows preserve saved, effective and pending states and optional rewards", () => {
  const row = activityFromDTO({
    id: 2222,
    name: "饺子大师",
    group: "活动入口",
    enabled: true,
    effectiveEnabled: false,
    pendingRestart: false,
    availabilityOnly: true,
    rewardSummary: "成长胶囊 × 1",
    rewards: [
      { name: "复活币", count: 2 },
      { itemId: "3037", quantity: "5" },
      "成长胶囊 × 1",
    ],
  });
  assert.equal(row.enabled, true);
  assert.equal(row.effectiveEnabled, false);
  assert.equal(row.pendingRestart, true);
  assert.equal(row.availabilityOnly, true);
  assert.deepEqual(row.rewards, ["成长胶囊 × 1", "复活币 × 2", "物品 3037 × 5"]);
  assert.deepEqual(filterActivities([row], "复活币").map((item) => item.id), [2222]);
  assert.deepEqual(filterActivities([row], "2222", "活动入口").map((item) => item.id), [2222]);
  assert.deepEqual(filterActivities([row], "3037", "在线与签到"), []);
});

test("refreshing keeps unsaved overrides for rows that still exist", () => {
  const rows = [
    activityFromDTO({ id: 350, name: "史诗之路", enabled: true, effectiveEnabled: true }),
    activityFromDTO({ id: 331, name: "普通签到", enabled: false, effectiveEnabled: false }),
  ];
  const draft = { "350": false, "999": true };
  const retained = pruneActivityDraft(rows, draft);
  assert.deepEqual(retained, { "350": false });
  assert.deepEqual(changedActivities(rows, retained).map((row) => row.id), [350]);
});

test("live activity adapter reads data.activityControls and restart capability", async () => {
  const api = new ConsoleAPI(() => {});
  api.http.fetcher = async (path) => {
    assert.equal(path, "/api/v1/activities");
    return envelope({
      restartAvailable: true,
      activityControls: {
        revision: 17,
        restartRequired: true,
        activities: [
          {
            id: 2347,
            name: "累计在线得好礼",
            enabled: true,
            effectiveEnabled: false,
            pendingRestart: true,
            group: "在线与签到",
          },
        ],
      },
    });
  };
  await api.activities();
  assert.equal(api.state.activityRevision, 17);
  assert.equal(api.state.activityRestartAvailable, true);
  assert.equal(api.state.activityRestartRequired, true);
  assert.equal(api.state.activities[0].pendingRestart, true);
});

test("activity management loads without a selected character", async () => {
  const api = new ConsoleAPI(() => {});
  api.meta.authenticated = true;
  const calls = [];
  api.http.fetcher = async (path) => {
    calls.push(path);
    return envelope({activityControls:{revision:4,restartRequired:false,activities:[]},restartAvailable:false});
  };
  await api.enter("gm");
  assert.deepEqual(calls,["/api/v1/activities"]);
});

test("activity save confirmation sends only the changed activity and preserves other defaults", async () => {
  const api = new ConsoleAPI(() => {});
  api.state.activities = [
    activityFromDTO({ id: 350, name: "史诗之路", enabled: true, effectiveEnabled: true }),
    activityFromDTO({ id: 331, name: "普通签到", enabled: true, effectiveEnabled: true }),
  ];
  api.state.activityRevision = 9;
  await api.prepare(
    {
      kind: "activity-save",
      revision: 9,
      activities: [
        { id: 350, enabled: false },
        { id: 331, enabled: true },
      ],
      changes: [{ id: 350, name: "史诗之路", enabled: false }],
    },
    { accounts: [], characters: [] },
  );
  assert.equal(api.meta.pending.steps.length, 1);
  assert.equal(api.meta.pending.steps[0].path, "/api/v1/activities");
  assert.deepEqual(api.meta.pending.steps[0].body, {
    revision: 9,
    activities: [
      { id: 350, enabled: false },
    ],
  });
  assert.match(api.meta.pending.summary, /关闭「史诗之路」/);
  api.cancel();
});

test("restart confirmation sends its own revision-guarded request", async () => {
  const api = new ConsoleAPI(() => {});
  api.state.activityRevision = 21;
  await api.prepare(
    { kind: "activity-restart", revision: 21 },
    { accounts: [], characters: [] },
  );
  const step = api.meta.pending.steps[0];
  assert.equal(step.path, "/api/v1/activities/restart");
  assert.deepEqual(step.body, { revision: 21 });
  assert.match(api.meta.pending.summary, /在线玩家需要重新登录/);
  api.cancel();
});

test("lost save response recovers only when the server readback matches the full draft", async () => {
  const calls = [];
  const api = new ConsoleAPI(() => {});
  api.http.fetcher = async (path, options = {}) => {
    calls.push(`${options.method || "GET"} ${path}`);
    if (path === "/api/v1/activities" && options.method === "POST")
      return envelope({}, 409);
    return envelope({
      restartAvailable: true,
      activityControls: {
        revision: 10,
        restartRequired: true,
        activities: [
          { id: 350, name: "史诗之路", enabled: false, effectiveEnabled: true, pendingRestart: true },
          { id: 331, name: "普通签到", enabled: true, effectiveEnabled: true, pendingRestart: false },
        ],
      },
    });
  };
  api.character = async () => {};
  api.enter = async () => {};
  api.state.activityRevision = 9;
  api.meta.pending = {
    id: "activity-request-1234567890",
    title: "确认活动设置",
    target: "游戏活动设置",
    summary: "保存活动开关",
    confirmed: false,
    steps: [
      {
        id: "activity-request-1234567890-0",
        label: "保存活动开关",
        path: "/api/v1/activities",
        body: {
          revision: 9,
          activities: [
            { id: 350, enabled: false },
            { id: 331, enabled: true },
          ],
        },
      },
    ],
  };
  await api.confirm();
  assert.equal(api.meta.pending.completed, true);
  assert.equal(api.state.activityRevision, 10);
  assert.deepEqual(calls, ["POST /api/v1/activities", "GET /api/v1/activities"]);
  api.cancel();
});

test("a duplicate restart response is treated as the already requested outcome", async () => {
  const api = new ConsoleAPI(() => {});
  api.http.fetcher = async () =>
    new Response(
      JSON.stringify({
        error: {
          code: "restart_already_requested",
          message: "本次重启已提交",
        },
      }),
      { status: 409 },
    );
  api.character = async () => {};
  api.enter = async () => {};
  api.state.activityRevision = 12;
  api.meta.pending = {
    id: "activity-restart-1234567890",
    title: "确认重启",
    target: "游戏服务器",
    summary: "请求重启",
    confirmed: false,
    steps: [
      {
        id: "activity-restart-1234567890-0",
        label: "请求重启游戏服务器",
        path: "/api/v1/activities/restart",
        body: { revision: 12 },
      },
    ],
  };
  await api.confirm();
  assert.equal(api.meta.pending.completed, true);
  assert.equal(api.state.activityRestartRequestedRevision, 12);
  api.cancel();
});

test("demo fixture saves a versioned draft and restart applies it", () => {
  const state = initialState();
  const saved = applyCommand(
    state,
    {
      kind: "activity-save",
      revision: state.activityRevision,
      activities: state.activities.map((row) => ({
        id: row.id,
        enabled: row.id === 350 ? false : row.enabled,
      })),
      changes: [{ id: 350, name: "史诗之路", enabled: false }],
    },
    "2026-10-20T03:00:00.000Z",
    "activity-save",
  );
  assert.equal(saved.activityRevision, state.activityRevision + 1);
  assert.equal(saved.activities.find((row) => row.id === 350).enabled, false);
  assert.equal(saved.activities.find((row) => row.id === 350).effectiveEnabled, true);
  assert.equal(saved.activityRestartRequired, true);
  assert.throws(
    () =>
      applyCommand(
        saved,
        {
          kind: "activity-restart",
          revision: state.activityRevision,
        },
        "2026-10-20T03:01:00.000Z",
        "stale-restart",
      ),
    /刷新后重新确认/,
  );
  const restarted = applyCommand(
    saved,
    { kind: "activity-restart", revision: saved.activityRevision },
    "2026-10-20T03:02:00.000Z",
    "activity-restart",
  );
  assert.equal(restarted.activityRestartRequired, false);
  assert.equal(restarted.activities.find((row) => row.id === 350).effectiveEnabled, false);
  assert.ok(restarted.activities.every((row) => !row.pendingRestart));
});
