import { NavLink, Navigate, Route, Routes } from "react-router-dom";
import { useData } from "./DataContext";
import { AccountBar } from "./components/AccountBar";
import { IssuesPage } from "./pages/IssuesPage";
import { BoardPage } from "./pages/BoardPage";
import { IssueDetailPage } from "./pages/IssueDetailPage";
import { StoriesPage } from "./pages/StoriesPage";
import { StoryDetailPage } from "./pages/StoryDetailPage";
import { EpicsPage } from "./pages/EpicsPage";
import { EpicDetailPage } from "./pages/EpicDetailPage";
import { TagsPage } from "./pages/TagsPage";
import { ProjectsPage } from "./pages/ProjectsPage";
import { ApiPage } from "./pages/ApiPage";

function Nav() {
  const { me } = useData();
  const items = [
    { to: "/issues", icon: "▤", label: "Issues" },
    { to: "/board", icon: "▦", label: "Board" },
    ...(me
      ? [
          { to: "/my-issues", icon: "👤", label: "My Issues" },
          { to: "/my-board", icon: "🙋", label: "My Board" },
        ]
      : []),
    { to: "/stories", icon: "🗂", label: "Stories" },
    { to: "/epics", icon: "📚", label: "Epics" },
  ];
  const settings = [
    { to: "/projects", icon: "📁", label: "Projects" },
    { to: "/tags", icon: "🏷", label: "Tags" },
    { to: "/api", icon: "🔌", label: "API" },
  ];
  return (
    <nav className="sidebar">
      <div className="brand">
        <span className="logo" />
        IssueTracker
      </div>
      <div className="nav-section">Workspace</div>
      {items.map((i) => (
        <NavLink
          key={i.to}
          to={i.to}
          className={({ isActive }) => `nav-item ${isActive ? "active" : ""}`}
        >
          <span className="icon">{i.icon}</span>
          {i.label}
        </NavLink>
      ))}
      <div className="nav-section">Settings</div>
      {settings.map((i) => (
        <NavLink
          key={i.to}
          to={i.to}
          className={({ isActive }) => `nav-item ${isActive ? "active" : ""}`}
        >
          <span className="icon">{i.icon}</span>
          {i.label}
        </NavLink>
      ))}
    </nav>
  );
}

export default function App() {
  const { loading } = useData();

  return (
    <div className="app">
      <Nav />
      <div className="main">
        <AccountBar />
        {loading ? (
          <div className="loading">Loading…</div>
        ) : (
          <Routes>
            <Route path="/" element={<Navigate to="/issues" replace />} />
            <Route path="/issues" element={<IssuesPage />} />
            <Route path="/board" element={<BoardPage />} />
            <Route path="/my-issues" element={<IssuesPage mine title="My Issues" />} />
            <Route path="/my-board" element={<BoardPage mine title="My Board" />} />
            <Route path="/issue/:ident" element={<IssueDetailPage />} />
            <Route path="/stories" element={<StoriesPage />} />
            <Route path="/stories/:id" element={<StoryDetailPage />} />
            <Route path="/epics" element={<EpicsPage />} />
            <Route path="/epics/:id" element={<EpicDetailPage />} />
            <Route path="/projects" element={<ProjectsPage />} />
            <Route path="/tags" element={<TagsPage />} />
            <Route path="/api" element={<ApiPage />} />
            <Route path="*" element={<Navigate to="/issues" replace />} />
          </Routes>
        )}
      </div>
    </div>
  );
}
