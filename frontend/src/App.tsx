import { BrowserRouter, Routes, Route, Navigate, useNavigate } from "react-router-dom"
import { AuthProvider, useAuth } from "./context/AuthContext"
import { ProtectedRoute, PublicOnlyRoute } from "./router/ProtectedRoute"
import LoginPage from "./pages/LoginPage"
import RegisterPage from "./pages/RegisterPage"
import DashboardPage from "./pages/DashboardPage"
import RoomPage from "./pages/RoomPage"

export default function App() {
  return (
    <AuthProvider>
      <BrowserRouter>
        <Routes>
          <Route element={<PublicOnlyRoute />}>
            <Route path="/login"    element={<LoginPage />} />
            <Route path="/register" element={<RegisterPage />} />
          </Route>

          <Route element={<ProtectedRoute />}>
            <Route path="/dashboard"   element={<DashboardPage />} />
            <Route path="/room/:roomId" element={<RoomPage />} />
          </Route>

          <Route path="*" element={<Navigate to="/dashboard" replace />} />
        </Routes>
      </BrowserRouter>
    </AuthProvider>
  )
}