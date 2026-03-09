import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom";
import Layout from "./components/Layout";
import ExportsPage from "./pages/ExportsPage";
import JobsPage from "./pages/JobsPage";
import PreviewPage from "./pages/PreviewPage";
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
          <Route path="/jobs" element={<JobsPage />} />
          <Route path="/preview" element={<PreviewPage />} />
          <Route path="/exports" element={<ExportsPage />} />
        </Route>
      </Routes>
    </BrowserRouter>
  );
}
