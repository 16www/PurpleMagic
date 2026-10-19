// Keep the user's draft verbatim until submission, including an empty field.
export const numberDraft = (value: number): string => Number.isFinite(value) ? String(value) : "";
export const draftNumber = (value: string): number => value.trim() === "" ? NaN : Number(value);
