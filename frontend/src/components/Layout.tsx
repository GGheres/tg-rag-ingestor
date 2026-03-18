import { NavLink, Outlet } from "react-router-dom";

const links = [{ to: "/sources", label: "Sources" }];

export default function Layout() {
  return (
    <div className="app-shell">
      <header className="topbar">
        <div className="topbar-title">
          <h1>RAG Ingestor Admin</h1>
          <p>Telegram parsing and YouTube audio extraction</p>
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
