import { useData } from "../DataContext";
import { Avatar } from "./common";

export function AccountBar() {
  const { me, whoami } = useData();

  if (!whoami?.authenticated) return null;

  return (
    <div className="account-bar">
      <div className="account-ident">
        <Avatar user={me} />
        <span className="account-name">{me?.name ?? whoami.identity}</span>
        <span className="account-via">{whoami.method}</span>
      </div>
    </div>
  );
}
