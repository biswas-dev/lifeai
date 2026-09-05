import type { ReactNode } from "react";
import { Link } from "react-router-dom";
import type { AuthConfig } from "../lib/types";
import { LeafIcon } from "../components/Icons";

export function AuthShell({
  title,
  subtitle,
  children,
}: {
  title: string;
  subtitle: string;
  children: ReactNode;
}) {
  return (
    <div className="flex min-h-dvh items-center justify-center px-4 py-12">
      <div className="w-full max-w-sm animate-slide-up">
        <div className="mb-8 text-center">
          <Link
            to="/"
            className="mx-auto mb-4 flex h-14 w-14 items-center justify-center rounded-2xl bg-vital-500/15 text-vital-400"
          >
            <LeafIcon size={28} />
          </Link>
          <h1 className="text-2xl font-semibold text-ink-100">{title}</h1>
          <p className="mt-1 text-sm text-ink-500">{subtitle}</p>
        </div>
        <div className="card p-6">{children}</div>
        <p className="mt-6 text-center text-xs text-ink-600">
          lifeai · one record of your life
        </p>
      </div>
    </div>
  );
}

export function OAuthButtons({ config }: { config: AuthConfig | null }) {
  if (!config || (!config.google && !config.github)) return null;
  return (
    <>
      <div className="my-6 flex items-center gap-3">
        <span className="h-px flex-1 bg-ink-800" />
        <span className="text-xs uppercase tracking-wide text-ink-600">or</span>
        <span className="h-px flex-1 bg-ink-800" />
      </div>
      <div className="space-y-3">
        {config.google && (
          <a href="/api/auth/google" className="btn-ghost w-full">
            <GoogleMark />
            Continue with Google
          </a>
        )}
        {config.github && (
          <a href="/api/auth/github" className="btn-ghost w-full">
            <GitHubMark />
            Continue with GitHub
          </a>
        )}
      </div>
    </>
  );
}

function GoogleMark() {
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" aria-hidden>
      <path
        fill="#4285F4"
        d="M23 12.3c0-.8-.1-1.6-.2-2.3H12v4.5h6.2a5.3 5.3 0 0 1-2.3 3.5v2.9h3.7c2.2-2 3.4-5 3.4-8.6Z"
      />
      <path
        fill="#34A853"
        d="M12 24c3.1 0 5.7-1 7.6-2.8l-3.7-2.9c-1 .7-2.3 1.1-3.9 1.1-3 0-5.5-2-6.4-4.7H1.8v3C3.7 21.4 7.6 24 12 24Z"
      />
      <path
        fill="#FBBC05"
        d="M5.6 14.7a7.2 7.2 0 0 1 0-4.6v-3H1.8a12 12 0 0 0 0 10.7l3.8-3Z"
      />
      <path
        fill="#EA4335"
        d="M12 4.8c1.7 0 3.2.6 4.4 1.7l3.3-3.3C17.7 1.2 15.1 0 12 0 7.6 0 3.7 2.6 1.8 6.2l3.8 3C6.5 6.7 9 4.8 12 4.8Z"
      />
    </svg>
  );
}

function GitHubMark() {
  return (
    <svg
      width="18"
      height="18"
      viewBox="0 0 24 24"
      fill="currentColor"
      aria-hidden
    >
      <path d="M12 .3a12 12 0 0 0-3.8 23.4c.6.1.8-.3.8-.6v-2c-3.3.7-4-1.6-4-1.6-.6-1.4-1.4-1.8-1.4-1.8-1.1-.7.1-.7.1-.7 1.2 0 1.9 1.2 1.9 1.2 1.1 1.8 2.8 1.3 3.5 1 .1-.8.4-1.3.8-1.6-2.7-.3-5.5-1.3-5.5-5.9 0-1.3.5-2.4 1.2-3.2-.1-.3-.5-1.5.1-3.2 0 0 1-.3 3.3 1.2a11.5 11.5 0 0 1 6 0C17.3 4.9 18.3 5.2 18.3 5.2c.6 1.7.2 2.9.1 3.2.8.8 1.2 1.9 1.2 3.2 0 4.6-2.8 5.6-5.5 5.9.4.4.8 1.1.8 2.2v3.3c0 .3.2.7.8.6A12 12 0 0 0 12 .3Z" />
    </svg>
  );
}
