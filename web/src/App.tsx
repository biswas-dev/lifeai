import { Suspense, lazy } from "react";
import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom";
import { Layout } from "./components/Layout";
import { AuthProvider, useAuth } from "./state/AuthContext";
import { Landing } from "./routes/Landing";
import { Login } from "./routes/Login";
import { Signup } from "./routes/Signup";
import { ForgotPassword, ResetPassword } from "./routes/ForgotPassword";
import { OAuthCallback } from "./routes/OAuthCallback";
import { Today } from "./routes/Today";
import { Privacy } from "./routes/Privacy";

const DayDetail = lazy(() =>
  import("./routes/DayDetail").then((m) => ({ default: m.DayDetail })),
);
const History = lazy(() =>
  import("./routes/History").then((m) => ({ default: m.History })),
);
const Recipes = lazy(() =>
  import("./routes/Recipes").then((m) => ({ default: m.Recipes })),
);
const RecipeDetail = lazy(() =>
  import("./routes/RecipeDetail").then((m) => ({ default: m.RecipeDetail })),
);
const Photos = lazy(() =>
  import("./routes/Photos").then((m) => ({ default: m.Photos })),
);
const Trends = lazy(() =>
  import("./routes/Trends").then((m) => ({ default: m.Trends })),
);
const Coach = lazy(() =>
  import("./routes/Coach").then((m) => ({ default: m.Coach })),
);
const Journal = lazy(() =>
  import("./routes/Journal").then((m) => ({ default: m.Journal })),
);
const Blood = lazy(() =>
  import("./routes/Blood").then((m) => ({ default: m.Blood })),
);
const Settings = lazy(() =>
  import("./routes/Settings").then((m) => ({ default: m.Settings })),
);

function ProtectedRoute({ children }: { children: JSX.Element }) {
  const { user, loading } = useAuth();
  if (loading) return <FullPageSpinner />;
  if (!user) return <Navigate to="/login" replace />;
  return children;
}

function RootRoute() {
  const { user, loading } = useAuth();
  if (loading) return <FullPageSpinner />;
  if (user) return <Navigate to="/app" replace />;
  return <Landing />;
}

function PublicOnly({ children }: { children: JSX.Element }) {
  const { user, loading } = useAuth();
  if (loading) return <FullPageSpinner />;
  if (user) return <Navigate to="/app" replace />;
  return children;
}

export function FullPageSpinner() {
  return (
    <div className="flex min-h-dvh items-center justify-center">
      <div className="h-8 w-8 animate-spin rounded-full border-2 border-ink-700 border-t-vital-500" />
    </div>
  );
}

export default function App() {
  return (
    <AuthProvider>
      <BrowserRouter>
        <Suspense fallback={<FullPageSpinner />}>
          <Routes>
            <Route path="/" element={<RootRoute />} />
            <Route path="/privacy" element={<Privacy />} />
            <Route
              path="/login"
              element={
                <PublicOnly>
                  <Login />
                </PublicOnly>
              }
            />
            <Route
              path="/signup"
              element={
                <PublicOnly>
                  <Signup />
                </PublicOnly>
              }
            />
            <Route
              path="/forgot-password"
              element={
                <PublicOnly>
                  <ForgotPassword />
                </PublicOnly>
              }
            />
            <Route path="/reset-password" element={<ResetPassword />} />
            <Route path="/oauth/callback" element={<OAuthCallback />} />

            <Route
              path="/app"
              element={
                <ProtectedRoute>
                  <Layout />
                </ProtectedRoute>
              }
            >
              <Route index element={<Today />} />
              <Route path="day/:date" element={<DayDetail />} />
              <Route path="history" element={<History />} />
              <Route path="recipes" element={<Recipes />} />
              <Route path="recipes/:id" element={<RecipeDetail />} />
              <Route path="photos" element={<Photos />} />
              <Route path="trends" element={<Trends />} />
              <Route path="coach" element={<Coach />} />
              <Route path="journal" element={<Journal />} />
              <Route path="blood" element={<Blood />} />
              <Route path="settings" element={<Settings />} />
            </Route>

            <Route path="*" element={<Navigate to="/app" replace />} />
          </Routes>
        </Suspense>
      </BrowserRouter>
    </AuthProvider>
  );
}
