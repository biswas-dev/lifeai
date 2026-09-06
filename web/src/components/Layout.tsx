import { useState } from "react";
import { Sheet } from "./ui";
import { NavLink, Outlet, Link, useLocation } from "react-router-dom";
import { useAuth } from "../state/AuthContext";
import {
  BookIcon,
  CalendarIcon,
  ChartIcon,
  DropIcon,
  HomeIcon,
  ImageIcon,
  LeafIcon,
  PenIcon,
  SparkIcon,
  UserIcon,
} from "./Icons";

const tabs = [
  { to: "/app", label: "Today", icon: HomeIcon, end: true },
  { to: "/app/history", label: "Calendar", icon: CalendarIcon },
  { to: "/app/recipes", label: "Recipes", icon: BookIcon },
  { to: "/app/trends", label: "Trends", icon: ChartIcon },
  { to: "/app/blood", label: "Blood work", icon: DropIcon },
  { to: "/app/photos", label: "Photo journal", icon: ImageIcon },
  { to: "/app/journal", label: "Reflections", icon: PenIcon },
  { to: "/app/coach", label: "Health insights", icon: SparkIcon },
  { to: "/app/settings", label: "Settings", icon: UserIcon },
];
const phoneTabs = [
  tabs[0],
  tabs[1],
  { ...tabs[5], label: "Photos" },
  { ...tabs[7], label: "Insights" },
];
export function Layout() {
  const { user, logout } = useAuth();
  const [moreOpen, setMoreOpen] = useState(false);
  const location = useLocation();
  const current =
    tabs.find((t) => t.to === location.pathname)?.label || "Your record";
  return (
    <div className="app-shell flex min-h-dvh w-full">
      <aside className="sticky top-0 hidden h-dvh w-[232px] shrink-0 flex-col border-r border-ink-800 bg-white px-4 py-6 md:flex">
        <Link
          to="/app"
          className="mb-8 flex items-center gap-2.5 px-2 text-xl font-semibold tracking-tight"
        >
          <span className="flex h-9 w-9 items-center justify-center rounded-xl bg-vital-500 text-white">
            <LeafIcon size={20} />
          </span>
          lifeai.
        </Link>
        <div className="mb-7 flex items-center gap-3 rounded-xl border border-ink-800 bg-ink-950 p-3">
          <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-ink-800 bg-white text-sm text-vital-500">
            {user?.name?.[0] || "Y"}
          </span>
          <div className="min-w-0">
            <p className="text-[10px] text-ink-500">Personal workspace</p>
            <p className="truncate text-xs font-medium">
              {user?.name || "Your health journal"}
            </p>
          </div>
        </div>
        <p className="mb-3 px-3 text-[9px] font-medium uppercase tracking-[0.17em] text-ink-500">
          Your health
        </p>
        <nav
          aria-label="Main navigation"
          className="flex flex-1 flex-col gap-1"
        >
          {tabs.map((t) => (
            <NavLink
              key={t.to}
              to={t.to}
              end={t.end}
              className={({ isActive }) =>
                `flex items-center gap-3 rounded-lg px-3 py-2.5 text-[13px] transition-colors ${isActive ? "bg-vital-500/10 font-medium text-vital-500" : "text-ink-400 hover:bg-ink-950 hover:text-ink-200"}`
              }
            >
              <t.icon size={18} />
              {t.label}
            </NavLink>
          ))}
        </nav>
        <div className="border-t border-ink-800 px-2 pt-4">
          <p className="truncate text-xs font-medium">
            {user?.name || "Your account"}
          </p>
          <p className="mt-1 truncate text-[10px] text-ink-500">
            {user?.email}
          </p>
        </div>
      </aside>
      <div className="min-w-0 flex-1">
        <header className="sticky top-0 z-30 flex h-14 items-center justify-between border-b border-ink-800 bg-white/95 px-4 backdrop-blur-lg md:static md:h-16 md:px-9">
          <div className="text-xs text-ink-500">
            <Link to="/app" className="md:hidden font-semibold text-vital-500">
              lifeai. <span className="mx-2">/</span>
            </Link>
            <span className="hidden md:inline">
              Personal workspace <span className="mx-3 text-ink-700">/</span>
            </span>
            <span className="text-ink-300">{current}</span>
          </div>
          <div className="flex items-center gap-5">
            <Link to="/app/journal" className="text-xs text-ink-500">
              Write a reflection
            </Link>
            <button
              onClick={logout}
              className="hidden text-xs text-ink-500 sm:block"
            >
              Sign out
            </button>
          </div>
        </header>
        <main
          id="main-content"
          className="mx-auto max-w-[1440px] px-4 pb-28 pt-5 md:px-9 md:pb-12 md:pt-9"
        >
          <Outlet />
        </main>
      </div>
      <nav
        aria-label="Mobile navigation"
        className="fixed inset-x-0 bottom-0 z-40 border-t border-ink-800 bg-white/95 backdrop-blur-lg md:hidden"
      >
        <div
          className="mx-auto flex max-w-lg items-stretch justify-around"
          style={{ paddingBottom: "env(safe-area-inset-bottom)" }}
        >
          {phoneTabs.map((t) => (
            <NavLink
              key={t.to}
              to={t.to}
              end={t.end}
              className={({ isActive }) =>
                `flex flex-1 flex-col items-center gap-1 py-3 text-[10px] ${isActive ? "font-semibold text-vital-500" : "text-ink-500"}`
              }
            >
              <t.icon size={19} />
              {t.label}
            </NavLink>
          ))}
          <button
            type="button"
            onClick={() => setMoreOpen(true)}
            aria-label="More navigation"
            aria-expanded={moreOpen}
            className={`flex flex-1 flex-col items-center gap-1 py-3 text-[10px] ${!phoneTabs.some((t) => t.to === location.pathname) && !location.pathname.startsWith("/app/day/") ? "font-semibold text-vital-500" : "text-ink-500"}`}
          >
            <UserIcon size={19} />
            More
          </button>
        </div>
      </nav>
      <Sheet
        open={moreOpen}
        onClose={() => setMoreOpen(false)}
        title="Your space"
      >
        <p className="mb-4 text-sm text-ink-500">{user?.name || "Lifeai"}</p>
        <nav aria-label="More navigation" className="grid grid-cols-2 gap-2">
          {tabs
            .filter((t) => !phoneTabs.some((p) => p.to === t.to))
            .map((t) => (
              <Link
                key={t.to}
                to={t.to}
                onClick={() => setMoreOpen(false)}
                className="flex min-h-16 items-center gap-3 rounded-xl border border-ink-800 p-3 text-sm"
              >
                <t.icon size={19} />
                {t.label}
              </Link>
            ))}
        </nav>
        <button
          type="button"
          className="btn-ghost mt-5 w-full"
          onClick={() => {
            setMoreOpen(false);
            logout();
          }}
        >
          Sign out
        </button>
      </Sheet>
    </div>
  );
}
