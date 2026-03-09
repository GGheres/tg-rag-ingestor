import { NavLink, Outlet } from "react-router-dom";

const links = [
  { to: "/sources", label: "Sources" },
  { to: "/jobs", label: "Jobs" },
  { to: "/preview", label: "Preview" },
  { to: "/exports", label: "Exports" },
];

export default function Layout() {
  return (
    <div className="app-shell">
      <header className="topbar">
        <div className="topbar-title">
          <h1>Telegram RAG Ingestor</h1>
          <p>MVP admin panel for source sync, processing, and exports</p>
        </div>
        <nav className="topbar-nav">
          {links.map((link) => (
            <NavLink
              key={link.to}
              to={link.to}
              className={({ isActive }) => (isActive ? "nav-link active" : "nav-link")}
            >
              {link.label}
            </NavLink>
          ))}
        </nav>
      </header>
      <main className="page-root">
        <Outlet />
      </main>
    </div>
  );
}
