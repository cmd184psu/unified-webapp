// Pure logic for choosing where an API key works. Run with `npm run test:web`.

/** The stored scope value meaning "every protected module". */
export const ALL = "*";

export interface Scope {
  all: boolean;
  modules: string[];
}

/** A stored scope. An empty list (a legacy key) works everywhere, so it reads as "all". */
export function scopeFromList(list: string[] | undefined): Scope {
  if (!list || list.length === 0 || list.includes(ALL)) return { all: true, modules: [] };
  return { all: false, modules: [...list].sort() };
}

export function scopeToList(s: Scope): string[] {
  return s.all ? [ALL] : [...s.modules].sort();
}

/** A key is never unscoped: "all modules" or at least one module. */
export function scopeValid(s: Scope): boolean {
  return s.all || s.modules.length > 0;
}

export function setAll(s: Scope, all: boolean): Scope {
  return { all, modules: s.modules };
}

export function setModule(s: Scope, module: string, on: boolean): Scope {
  const without = s.modules.filter((m) => m !== module);
  return { all: s.all, modules: on ? [...without, module].sort() : without };
}

/** Short text for a scope, for tooltips and the list. */
export function scopeSummary(list: string[] | undefined): string {
  const s = scopeFromList(list);
  return s.all ? "All modules" : s.modules.join(", ");
}
