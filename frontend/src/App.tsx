import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom";
import Layout from "./components/Layout";
import ImportSourcesPage from "./pages/ImportSourcesPage";

export default function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/" element={<Layout />}>
          <Route index element={<Navigate to="/import" replace />} />
          <Route path="/import" element={<ImportSourcesPage />} />
          <Route path="*" element={<Navigate to="/import" replace />} />
        </Route>
      </Routes>
    </BrowserRouter>
  );
}
