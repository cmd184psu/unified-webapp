import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useState,
  type ReactNode,
} from "react";
import { api } from "./api";
import type { Bootstrap, User, Tag, Team, Whoami } from "./types";

interface DataState {
  loading: boolean;
  teams: Team[];
  users: User[];
  tags: Tag[];
  whoami: Whoami | null;
  me: User | null;
  /** Display name for a user, rendering the current user as "Me". */
  userLabel: (u: User) => string;
  reloadTags: () => Promise<void>;
  reloadTeams: () => Promise<void>;
  reloadUsers: () => Promise<void>;
}

const Ctx = createContext<DataState | null>(null);

export function DataProvider({ children }: { children: ReactNode }) {
  const [data, setData] = useState<Bootstrap | null>(null);
  const [whoami, setWhoami] = useState<Whoami | null>(null);

  useEffect(() => {
    api.bootstrap().then(setData).catch((e) => console.error(e));
    api.whoami().then(setWhoami).catch((e) => console.error(e));
  }, []);

  const me =
    (whoami?.authenticated &&
      data?.users.find((u) => u.username === whoami.identity)) ||
    null;

  const userLabel = useCallback(
    (u: User) => (me && u.id === me.id ? "Me" : u.name),
    [me?.id]
  );

  const reloadTags = useCallback(async () => {
    const b = await api.bootstrap();
    setData((d) => (d ? { ...d, tags: b.tags } : b));
  }, []);
  const reloadTeams = useCallback(async () => {
    const b = await api.bootstrap();
    setData((d) => (d ? { ...d, teams: b.teams } : b));
  }, []);
  const reloadUsers = useCallback(async () => {
    const b = await api.bootstrap();
    setData((d) => (d ? { ...d, users: b.users } : b));
  }, []);

  const value: DataState = {
    loading: !data,
    teams: data?.teams ?? [],
    users: data?.users ?? [],
    tags: data?.tags ?? [],
    whoami,
    me,
    userLabel,
    reloadTags,
    reloadTeams,
    reloadUsers,
  };

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useData(): DataState {
  const ctx = useContext(Ctx);
  if (!ctx) throw new Error("useData must be used within DataProvider");
  return ctx;
}
