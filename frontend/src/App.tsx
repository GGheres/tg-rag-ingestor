import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom";
import Layout from "./components/Layout";
import HHResumesPage from "./pages/HHResumesPage";
import SourceDetailsPage from "./pages/SourceDetailsPage";
import SourcesPage from "./pages/SourcesPage";

export default function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/" element={<Layout />}>
          <Route index element={<Navigate to="/sources" replace />} />
          <Route path="/sources" element={<SourcesPage />} />
          <Route path="/sources/:id" element={<SourceDetailsPage />} />
          <Route path="/hh-resumes" element={<HHResumesPage />} />
          <Route path="*" element={<Navigate to="/sources" replace />} />
        </Route>
      </Routes>
    </BrowserRouter>
  );
}
