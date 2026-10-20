import type { Activity } from "./domain";

export type ActivityDTO = Record<string, unknown>;
export type ActivityDraft = Record<string, boolean>;

function strings(value: unknown): string[] {
  if (value == null || value === "") return [];
  if (Array.isArray(value)) return value.flatMap(strings);
  if (typeof value === "object") {
    const row = value as Record<string, unknown>;
    const out: string[] = [];
    const label =
      row.label || row.name || row.itemName || row.summary || row.description || row.text;
    const count = row.count ?? row.quantity;
    if (label)
      out.push(
        count == null || count === ""
          ? String(label)
          : `${String(label)} × ${String(count)}`,
      );
    else if (row.itemId != null)
      out.push(
        `物品 ${String(row.itemId)}` +
          (count == null || count === "" ? "" : ` × ${String(count)}`),
      );
    if (row.gold != null && Number.isFinite(Number(row.gold)) && Number(row.gold) !== 0)
      out.push(`金币 ${Number(row.gold).toLocaleString("zh-CN")}`);
    out.push(...strings(row.items));
    out.push(...strings(row.attachments));
    out.push(...strings(row.rewardItems));
    return out;
  }
  return [String(value)];
}

export function activityRewards(row: ActivityDTO): string[] {
  return [
    ...new Set(
      [
        ...strings(row.rewardSummary),
        ...strings(row.rewardDescription),
        ...strings(row.rewards),
        ...strings(row.reward),
        ...strings(row.rewardItems),
      ]
        .map((s) => s.trim())
        .filter(Boolean),
    ),
  ];
}

export function activityFromDTO(row: ActivityDTO): Activity {
  const enabled = row.enabled === true;
  const effectiveEnabled =
    row.effectiveEnabled === undefined ? enabled : row.effectiveEnabled === true;
  return {
    id: Number(row.id),
    name: String(row.name || `活动 ${row.id}`),
    description: String(row.description || ""),
    source: row.source == null ? "" : String(row.source),
    group: row.group == null ? "" : String(row.group),
    availabilityOnly: row.availabilityOnly === true,
    defaultEnabled: row.defaultEnabled === true,
    enabled,
    effectiveEnabled,
    pendingRestart: row.pendingRestart === true || enabled !== effectiveEnabled,
    rewards: activityRewards(row),
  };
}

export function activitySnapshot(data: ActivityDTO): {
  snapshot: ActivityDTO;
  restartAvailable: boolean;
} {
  const snapshot =
    data.activityControls && typeof data.activityControls === "object"
      ? (data.activityControls as ActivityDTO)
      : data;
  return {
    snapshot,
    restartAvailable:
      data.restartAvailable === true || snapshot.restartAvailable === true,
  };
}

export function activityDraft(rows: Activity[]): ActivityDraft {
  return Object.fromEntries(rows.map((row) => [String(row.id), row.enabled]));
}

export function pruneActivityDraft(
  rows: Activity[],
  draft: ActivityDraft,
): ActivityDraft {
  return Object.fromEntries(
    rows
      .filter((row) => Object.hasOwn(draft, String(row.id)))
      .map((row) => [String(row.id), draft[String(row.id)]]),
  );
}

export function activityValue(row: Activity, draft: ActivityDraft): boolean {
  return Object.hasOwn(draft, String(row.id)) ? draft[String(row.id)] : row.enabled;
}

export function changedActivities(
  rows: Activity[],
  draft: ActivityDraft,
): Activity[] {
  return rows.filter((row) => activityValue(row, draft) !== row.enabled);
}

export function activitySearchText(row: Activity): string {
  return [
    row.id,
    row.name,
    row.description,
    row.group,
    row.source,
    row.availabilityOnly ? "入口可见性" : "",
    ...row.rewards,
  ]
    .join("\n")
    .toLocaleLowerCase("zh-CN");
}

export function filterActivities(
  rows: Activity[],
  query: string,
  group = "",
): Activity[] {
  const q = query.trim().toLocaleLowerCase("zh-CN");
  return rows.filter(
    (row) =>
      (!group || (row.group || "活动") === group) &&
      (!q || activitySearchText(row).includes(q)),
  );
}
