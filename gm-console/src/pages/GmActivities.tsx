import { useEffect, useMemo, useState } from "react";
import { Power, RefreshCw, RotateCcw, Save } from "lucide-react";
import { useLive } from "../api/context";
import {
  activityValue,
  changedActivities,
  filterActivities,
  pruneActivityDraft,
  type ActivityDraft,
} from "../activity";
import type { Activity, Command, State } from "../domain";
import {
  CheckBox,
  Empty,
  Pill,
  SearchField,
  number,
} from "../ui";

const activityDraftKey = "purplemagic-activity-draft-v1";

function savedDraft(): ActivityDraft {
  try {
    const value = JSON.parse(sessionStorage.getItem(activityDraftKey) || "{}");
    return value?.values && typeof value.values === "object"
      ? value.values
      : {};
  } catch {
    return {};
  }
}

function statusPill(value: boolean) {
  return <Pill tone={value ? "green" : "neutral"}>{value ? "启用" : "暂停"}</Pill>;
}

export function GmActivities({
  state,
  request,
  notify,
}: {
  state: State;
  request: (command: Command) => void;
  notify: (message: string) => void;
}) {
  const live = useLive();
  const rows = state.activities;
  const [draft, setDraft] = useState<ActivityDraft>(savedDraft);
  const [query, setQuery] = useState("");
  const [group, setGroup] = useState("全部");
  const [preserved, setPreserved] = useState(false);
  const groups = useMemo(
    () => ["全部", ...new Set(rows.map((row) => row.group || "活动"))],
    [rows],
  );
  const visible = useMemo(
    () => filterActivities(rows, query, group === "全部" ? "" : group),
    [rows, query, group],
  );
  const changes = changedActivities(rows, draft);
  const pending = state.activityRestartRequired || rows.some((row) => row.pendingRestart);
  const requested =
    state.activityRestartRequestedRevision === state.activityRevision;

  useEffect(() => {
    setDraft((previous) => {
      const retained = pruneActivityDraft(rows, previous);
      return Object.fromEntries(
        Object.entries(retained).filter(([id, value]) => {
          const row = rows.find((item) => String(item.id) === id);
          return row && row.enabled !== value;
        }),
      );
    });
  }, [rows.map((row) => `${row.id}:${row.enabled}`).join("|")]);

  useEffect(() => {
    if (!rows.length) return;
    if (changes.length)
      sessionStorage.setItem(
        activityDraftKey,
        JSON.stringify({
          revision: state.activityRevision,
          values: draft,
        }),
      );
    else sessionStorage.removeItem(activityDraftKey);
  }, [draft, changes.length, rows.length, state.activityRevision]);

  function toggle(row: Activity) {
    const next = !activityValue(row, draft);
    setDraft((previous) => {
      const value = { ...previous, [String(row.id)]: next };
      if (next === row.enabled) delete value[String(row.id)];
      return value;
    });
    setPreserved(false);
  }

  async function refresh() {
    if (!live) {
      notify("演示模式已保留当前草稿");
      return;
    }
    const hadDraft = changes.length > 0;
    await live.activities();
    if (hadDraft) setPreserved(true);
  }

  function save() {
    if (!changes.length) {
      notify("没有需要保存的活动修改");
      return;
    }
    request({
      kind: "activity-save",
      revision: state.activityRevision,
      activities: changes.map((row) => ({
        id: row.id,
        enabled: activityValue(row, draft),
      })),
      changes: changes.map((row) => ({
        id: row.id,
        name: row.name,
        enabled: activityValue(row, draft),
      })),
    });
  }

  function discardDraft() {
    setDraft({});
    sessionStorage.removeItem(activityDraftKey);
    setPreserved(false);
    notify("已撤销未保存的活动草稿");
  }

  function restart() {
    request({ kind: "activity-restart", revision: state.activityRevision });
  }

  return (
    <section className="activity-workspace">
      <div className="workspace-intro">
        <div>
          <h2>游戏活动</h2>
          <p>活动开关先保存为服务端版本；实际运行状态和待重启状态分开显示。</p>
        </div>
        <div className="button-row activity-head-actions">
          <button
            type="button"
            className="secondary"
            disabled={!!live && !!state.api?.reading}
            onClick={() =>
              live
                ? void live.guard(refresh)
                : void refresh()
            }
          >
            <RefreshCw size={15} />
            刷新
          </button>
          <button
            type="button"
            className="primary"
            disabled={!changes.length || !!state.api?.busy}
            onClick={save}
          >
            <Save size={15} />
            保存设置{changes.length ? ` · ${changes.length}` : ""}
          </button>
        </div>
      </div>

      <div className="toolbar activity-toolbar">
        <SearchField
          value={query}
          onChange={setQuery}
          placeholder="搜索活动名称、编号、分组或奖励"
          label="搜索游戏活动"
        />
        <select
          aria-label="筛选活动分组"
          value={group}
          onChange={(event) => setGroup(event.target.value)}
        >
          {groups.map((name) => (
            <option key={name} value={name}>
              {name}
            </option>
          ))}
        </select>
        <span className="muted">
          {number(visible.length)} / {number(rows.length)} 项
        </span>
      </div>

      {preserved && (
        <p className="activity-refresh-note" role="status">
          已刷新服务端状态；未保存草稿仍保留，已保存与实际运行状态来自最新响应。
        </p>
      )}

      <div className="table-panel glass activity-table">
        <div className="table-scroll">
          <table>
            <thead>
              <tr>
                <th>活动</th>
                <th>奖励</th>
                <th>已保存</th>
                <th>实际运行</th>
                <th>待重启</th>
                <th>启停草稿</th>
              </tr>
            </thead>
            <tbody>
              {visible.map((row) => {
                const value = activityValue(row, draft);
                return (
                  <tr key={row.id} className={value !== row.enabled ? "selected" : ""}>
                    <td className="activity-name-cell">
                      <strong>{row.name}</strong>
                      <small className="cell-meta">
                        #{row.id} · {row.group || "活动"}
                        {row.availabilityOnly ? " · 入口可见性" : ""}
                      </small>
                      {row.description && <p>{row.description}</p>}
                      {row.source && <small className="activity-source">{row.source}</small>}
                    </td>
                    <td className="activity-rewards">
                      {row.rewards.length ? (
                        <div className="activity-reward-list">
                          {row.rewards.map((reward) => (
                            <span key={reward}>{reward}</span>
                          ))}
                        </div>
                      ) : (
                        <span className="muted">未随接口提供</span>
                      )}
                      {row.availabilityOnly && (
                        <small className="cell-meta">入口开启不保证玩法与奖励完整可用</small>
                      )}
                    </td>
                    <td>{statusPill(row.enabled)}</td>
                    <td>{statusPill(row.effectiveEnabled)}</td>
                    <td>
                      <Pill tone={row.pendingRestart ? "gold" : "neutral"}>
                        {row.pendingRestart ? "待重启" : "已同步"}
                      </Pill>
                    </td>
                    <td>
                      <label className="activity-draft-toggle">
                        <CheckBox
                          label={`${row.name} 启停草稿`}
                          checked={value}
                          onChange={() => toggle(row)}
                        />
                        <span>{value ? "启用" : "暂停"}</span>
                      </label>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
        {!visible.length && (
          <Empty
            title={rows.length ? "没有匹配的活动" : "暂无活动设置"}
            detail={rows.length ? "调整搜索词或活动分组后重试。" : undefined}
          />
        )}
        <div className="activity-savebar">
          <div>
            <strong>
              {changes.length
                ? `${changes.length} 项未保存草稿`
                : "草稿已清空"}
            </strong>
            <small>
              服务端版本 {number(state.activityRevision)}
              {pending ? " · 存在待生效设置" : " · 当前设置已同步"}
            </small>
          </div>
          <div className="button-row">
            {changes.length > 0 && (
              <button type="button" className="secondary" onClick={discardDraft}>
                <RotateCcw size={14} />
                撤销草稿
              </button>
            )}
            {state.activityRestartAvailable && pending && (
              <button
                type="button"
                className="primary"
                disabled={requested || !!state.api?.busy}
                onClick={restart}
              >
                <Power size={15} />
                {requested ? "重启已请求" : "重启并生效"}
              </button>
            )}
          </div>
        </div>
      </div>

      {pending && !state.activityRestartAvailable && (
        <p className="activity-restart-note">
          当前后端未提供活动重启接口，请由管理员重启游戏服务后生效。
        </p>
      )}
      {pending && requested && (
        <p className="activity-restart-note">
          已提交重启请求；实际运行状态会在游戏服务重启并刷新后更新。
        </p>
      )}
    </section>
  );
}
