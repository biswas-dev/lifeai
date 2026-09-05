import { useState } from "react";
import { Link } from "react-router-dom";
import {
  BookIcon,
  CameraIcon,
  DropIcon,
  LeafIcon,
  LinkIcon,
  SparkIcon,
  DumbbellIcon,
  PenIcon,
  HomeIcon,
} from "../components/Icons";
import { Sparkline } from "../components/Sparkline";

const features = [
  {
    icon: CameraIcon,
    title: "A photo. A meal. Logged.",
    body: "Capture your plate and get an editable nutrition estimate. Keep the photo alongside the numbers.",
  },
  {
    icon: BookIcon,
    title: "Recipes you’ll return to.",
    body: "Save your favourites, import a recipe, and log a serving straight into your day.",
  },
  {
    icon: DropIcon,
    title: "Your blood work, connected.",
    body: "Keep lab PDFs, review extracted markers, and follow each result across reports.",
  },
  {
    icon: DumbbellIcon,
    title: "Room for movement. And rest.",
    body: "Workouts, meditation, sleep and steps. See how the parts of your day fit together.",
  },
  {
    icon: PenIcon,
    title: "More than the numbers.",
    body: "Write a reflection next to your metrics. Find the patterns a chart alone can’t show.",
  },
  {
    icon: LinkIcon,
    title: "Bring your own intelligence.",
    body: "Use your own AI through MCP, or ask the built-in coach when you want another perspective.",
  },
];
const demoSeries = [
  77.8, 77.6, 77.9, 77.4, 77.5, 77.1, 77.3, 76.9, 77.0, 76.7, 76.9, 76.6,
].map((value, i) => ({ date: `Week ${i + 1}`, value }));

function Preview() {
  const [tab, setTab] = useState("Overview");
  return (
    <div className="mx-auto max-w-5xl overflow-hidden rounded-2xl border border-ink-800 bg-ink-950 text-left shadow-[0_24px_80px_-28px_rgba(35,60,44,0.18)]">
      <div className="flex items-center justify-between border-b border-ink-800 bg-white px-5 py-3 text-xs">
        <div className="flex items-center gap-2 font-semibold">
          <LeafIcon size={16} /> lifeai
          <span className="ml-2 font-normal text-ink-500">
            Personal workspace
          </span>
        </div>
        <span className="rounded-full bg-ink-850 px-2.5 py-1 text-[10px] text-ink-500">
          Illustrative data
        </span>
      </div>
      <div className="flex">
        <aside className="hidden w-44 shrink-0 border-r border-ink-800 bg-white p-4 sm:block">
          <p className="eyebrow mb-4 text-[9px]">YOUR SPACE</p>
          {["Overview", "Nutrition", "Blood work"].map((t) => (
            <button
              key={t}
              onClick={() => setTab(t)}
              className={`mb-1 flex w-full items-center gap-2 rounded-lg px-3 py-2.5 text-xs ${tab === t ? "bg-vital-500/10 font-medium text-vital-500" : "text-ink-400"}`}
            >
              {t === "Overview" ? (
                <HomeIcon size={14} />
              ) : t === "Nutrition" ? (
                <BookIcon size={14} />
              ) : (
                <DropIcon size={14} />
              )}
              {t}
            </button>
          ))}
          <div className="mt-5 space-y-4 px-3 text-xs text-ink-500">
            <p>Photo journal</p>
            <p>Trends</p>
            <p>Connections</p>
          </div>
        </aside>
        <div className="min-w-0 flex-1 p-5 sm:p-7">
          <div className="mb-5 flex items-center justify-between">
            <div>
              <p className="eyebrow mb-1 text-[9px]">
                A LITTLE BETTER, EVERY DAY
              </p>
              <h2 className="text-xl font-semibold tracking-tight">
                {tab === "Overview"
                  ? "Your day, at a glance."
                  : tab === "Nutrition"
                    ? "Small choices. A clear picture."
                    : "A longer view of your health."}
              </h2>
            </div>
            <span className="hidden text-[10px] text-ink-500 md:block">
              Saturday, 5 September
            </span>
          </div>
          <div className="mb-4 flex gap-2 sm:hidden">
            {["Overview", "Nutrition", "Blood work"].map((t) => (
              <button
                key={t}
                className={`chip ${tab === t ? "chip-active" : ""}`}
                onClick={() => setTab(t)}
              >
                {t}
              </button>
            ))}
          </div>
          {tab === "Blood work" ? (
            <div className="card overflow-hidden">
              <div className="border-b border-ink-800 px-4 py-3 text-xs font-medium">
                Lab markers · example report
              </div>
              {[
                ["HbA1c", "5.2", "%"],
                ["LDL cholesterol", "2.4", "mmol/L"],
                ["ALT", "24", "U/L"],
              ].map(([label, value, unit]) => (
                <div
                  className="flex justify-between border-b border-ink-800 px-4 py-5 last:border-0"
                  key={label}
                >
                  <span className="text-sm">{label}</span>
                  <span className="font-medium">
                    {value}{" "}
                    <span className="text-xs font-normal text-ink-500">
                      {unit}
                    </span>
                  </span>
                </div>
              ))}
              <p className="px-4 py-3 text-[10px] text-ink-500">
                Original reports and reference ranges stay with every reading.
              </p>
            </div>
          ) : (
            <>
              <div className="grid grid-cols-3 gap-3">
                {[
                  ["Daily intake", "1,480", "of 2,000 kcal"],
                  ["Movement", "42", "minutes today"],
                  ["Sleep", "7.5", "hours last night"],
                ].map(([label, value, sub]) => (
                  <div key={label} className="card p-3 sm:p-4">
                    <p className="text-[10px] text-ink-500">{label}</p>
                    <p className="my-1.5 text-2xl font-semibold tracking-tight sm:text-3xl">
                      {value}
                    </p>
                    <p className="text-[10px] text-ink-500">{sub}</p>
                  </div>
                ))}
              </div>
              <div className="mt-4 grid gap-4 md:grid-cols-2">
                <div className="card p-4">
                  <div className="mb-4 flex justify-between text-xs font-medium">
                    <span>
                      {tab === "Nutrition"
                        ? "Today’s nutrition"
                        : "Your progress"}
                    </span>
                    <span className="font-normal text-ink-500">
                      {tab === "Nutrition" ? "Logged so far" : "12 weeks"}
                    </span>
                  </div>
                  {tab === "Nutrition" ? (
                    <div className="space-y-4">
                      {[
                        ["Protein", "102 g", "71%"],
                        ["Carbs", "164 g", "65%"],
                        ["Fat", "46 g", "69%"],
                      ].map(([label, value, width]) => (
                        <div key={label}>
                          <div className="mb-1 flex justify-between text-xs text-ink-400">
                            <span>{label}</span>
                            <span>{value}</span>
                          </div>
                          <div className="h-1.5 rounded-full bg-ink-850">
                            <div
                              className="h-full rounded-full bg-vital-500"
                              style={{ width }}
                            />
                          </div>
                        </div>
                      ))}
                    </div>
                  ) : (
                    <>
                      <Sparkline points={demoSeries} unit="kg" height={90} />
                      <p className="mt-3 text-[10px] text-ink-500">
                        Weight · a pattern across weeks
                      </p>
                    </>
                  )}
                </div>
                <div className="card p-4">
                  <p className="mb-4 text-xs font-medium">
                    A few moments from today
                  </p>
                  {[
                    ["08:15", "Oats, yogurt & berries", "Breakfast"],
                    ["12:30", "Roasted vegetable bowl", "Lunch"],
                    ["17:00", "A walk outside", "Movement"],
                  ].map(([time, title, sub]) => (
                    <div
                      key={time}
                      className="flex gap-3 border-t border-ink-800 py-3 first:border-0"
                    >
                      <span className="pt-0.5 font-mono text-[9px] text-ink-500">
                        {time}
                      </span>
                      <div>
                        <p className="text-xs font-medium">{title}</p>
                        <p className="mt-0.5 text-[10px] text-ink-500">{sub}</p>
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            </>
          )}
        </div>
      </div>
    </div>
  );
}

export function Landing() {
  return (
    <div className="min-h-dvh bg-white text-ink-200">
      <a className="sr-only focus:not-sr-only" href="#main">
        Skip to content
      </a>
      <nav className="border-b border-ink-800 bg-white">
        <div className="mx-auto flex h-20 max-w-6xl items-center justify-between px-6">
          <Link
            to="/"
            className="flex items-center gap-2.5 text-xl font-semibold tracking-tight"
          >
            <span className="flex h-9 w-9 items-center justify-center rounded-xl bg-vital-500 text-white">
              <LeafIcon size={20} />
            </span>
            lifeai<span className="text-vital-500">.</span>
          </Link>
          <div className="hidden items-center gap-8 text-[13px] text-ink-300 md:flex">
            <a href="#features">Product</a>
            <a href="#how">How it works</a>
            <a href="#connections">Connections</a>
            <a href="#agents">For your AI</a>
          </div>
          <div className="flex items-center gap-5 text-[13px]">
            <Link className="hidden sm:block" to="/login">
              Sign in
            </Link>
            <Link to="/signup" className="btn-primary">
              Get started <span className="ml-3">↗</span>
            </Link>
          </div>
        </div>
      </nav>
      <main id="main">
        <section className="mx-auto max-w-6xl px-6 pb-20 pt-20 text-center md:pt-24">
          <p className="eyebrow">A clearer view of your everyday health</p>
          <h1 className="mx-auto mt-8 max-w-4xl text-[44px] font-semibold leading-[1.07] tracking-[-0.055em] text-ink-100 sm:text-6xl md:text-[76px]">
            Live a little better.
            <br />
            <span className="text-vital-500">See the whole picture.</span>
          </h1>
          <p className="mx-auto mt-7 max-w-2xl text-lg leading-relaxed text-ink-400">
            Your food, movement, body and blood work. Connected in one calm
            space, so the next small step is easier to see.
          </p>
          <div className="mt-8 flex flex-wrap justify-center gap-3">
            <Link className="btn-primary px-6 py-3.5" to="/signup">
              Start your health journal <span className="ml-3">↗</span>
            </Link>
            <a className="btn-ghost bg-white px-6 py-3.5" href="#preview">
              Explore the preview <span className="ml-3">→</span>
            </a>
          </div>
          <p className="mt-4 text-xs text-ink-500">
            Start free. Build your record at your own pace.
          </p>
          <div id="connections" className="py-14">
            <p className="mb-5 text-xs text-ink-500">
              A home for the health data you already have
            </p>
            <div className="flex flex-wrap items-center justify-center gap-x-9 gap-y-4 text-sm font-semibold text-ink-400 sm:text-base">
              {[
                "Apple Health",
                "Samsung Health",
                "Strava",
                "Lab reports",
                "MCP",
              ].map((name) => (
                <span key={name}>{name}</span>
              ))}
            </div>
            <p className="mt-3 text-[11px] text-ink-500">
              Health file imports · Strava connection · Personal AI access
            </p>
          </div>
          <div id="preview" className="scroll-mt-6">
            <Preview />
          </div>
          <p className="mt-5 text-xs text-ink-500">
            Try the tabs above. Your own workspace starts with your own data.
          </p>
        </section>
        <div className="border-y border-ink-800 bg-ink-950">
          <div className="mx-auto grid max-w-6xl gap-8 px-6 py-10 sm:grid-cols-3">
            {[
              [
                "One personal record",
                "Your daily habits, with the context that matters.",
              ],
              [
                "A longer perspective",
                "Watch trends across days, weeks and lab reports.",
              ],
              [
                "Your choice of AI",
                "Ask the built-in coach or connect your own assistant.",
              ],
            ].map(([title, body]) => (
              <div key={title}>
                <h2 className="text-sm font-semibold">{title}</h2>
                <p className="mt-2 text-sm text-ink-400">{body}</p>
              </div>
            ))}
          </div>
        </div>
        <section id="features" className="mx-auto max-w-6xl px-6 py-24">
          <p className="eyebrow">The pieces, brought together</p>
          <h2 className="section-title mt-4 max-w-2xl">
            A space for every part
            <br className="hidden sm:block" /> of feeling better.
          </h2>
          <div className="mt-12 grid gap-x-10 gap-y-12 sm:grid-cols-2 lg:grid-cols-3">
            {features.map((f) => (
              <article key={f.title}>
                <span className="mb-4 inline-flex h-10 w-10 items-center justify-center rounded-xl border border-ink-800 bg-ink-950 text-vital-500">
                  <f.icon size={19} />
                </span>
                <h3 className="text-lg font-semibold tracking-tight">
                  {f.title}
                </h3>
                <p className="mt-2 text-sm leading-relaxed text-ink-400">
                  {f.body}
                </p>
              </article>
            ))}
          </div>
        </section>
        <section id="how" className="border-y border-ink-800 bg-ink-950">
          <div className="mx-auto grid max-w-6xl gap-14 px-6 py-24 md:grid-cols-2">
            <div>
              <p className="eyebrow">Built around your day</p>
              <h2 className="section-title mt-4">
                Small habits.
                <br />A clearer direction.
              </h2>
              <p className="mt-6 max-w-md text-ink-400">
                You don’t need a perfect week to learn something useful. Start
                with what you have, then keep going.
              </p>
              <Link
                to="/signup"
                className="mt-7 inline-block text-sm font-medium text-vital-500"
              >
                Make room for your first day →
              </Link>
            </div>
            <ol className="space-y-8">
              {[
                [
                  "Capture the everyday.",
                  "A meal photo, a short reflection, a workout. Import from your health apps when it helps.",
                ],
                [
                  "Keep your health in context.",
                  "Review lab results beside your habits, with the original report always available.",
                ],
                [
                  "Find your next step.",
                  "Look at your trends, adjust your goals, and ask for a little help when you need it.",
                ],
              ].map(([title, body], i) => (
                <li className="flex gap-5" key={title}>
                  <span className="pt-1 font-mono text-xs text-vital-500">
                    0{i + 1}
                  </span>
                  <div>
                    <h3 className="text-xl font-semibold tracking-tight">
                      {title}
                    </h3>
                    <p className="mt-2 text-sm leading-relaxed text-ink-400">
                      {body}
                    </p>
                  </div>
                </li>
              ))}
            </ol>
          </div>
        </section>
        <section
          id="agents"
          className="mx-auto grid max-w-6xl items-center gap-12 px-6 py-24 md:grid-cols-2"
        >
          <div>
            <p className="eyebrow">Your data. Your assistant.</p>
            <h2 className="section-title mt-4">
              Less retyping.
              <br />
              More understanding.
            </h2>
            <p className="mt-5 text-ink-400">
              Give your preferred AI permission to read your record through MCP.
              Your summary is computed from saved data, without an extra model
              call from lifeai.
            </p>
            <Link
              to="/signup"
              className="mt-6 inline-block text-sm font-medium text-vital-500"
            >
              Create your workspace →
            </Link>
          </div>
          <div className="rounded-2xl border border-ink-800 bg-ink-950 p-6">
            <div className="flex items-center gap-2 border-b border-ink-800 pb-4 text-sm font-medium">
              <SparkIcon size={18} /> A conversation with your record
              <span className="ml-auto text-[10px] text-ink-500">Example</span>
            </div>
            <p className="my-6 rounded-xl border border-ink-800 bg-white p-4 text-sm">
              “How have my sleep, meals and training changed this month?”
            </p>
            <div className="space-y-3 font-mono text-xs text-vital-500">
              <p>✓ Read your health summary</p>
              <p>✓ Compare recorded days</p>
              <p>✓ Bring your lab trends into context</p>
            </div>
            <p className="mt-6 text-xs text-ink-500">
              Read-only tokens by default. Revoke access anytime.
            </p>
          </div>
        </section>
        <section className="border-t border-ink-800 bg-[#eef4ef] px-6 py-20 text-center">
          <p className="eyebrow">Begin where you are</p>
          <h2 className="section-title mt-4">
            Your next chapter
            <br />
            starts with today.
          </h2>
          <Link className="btn-primary mt-8 px-7 py-3.5" to="/signup">
            Get started free <span className="ml-4">↗</span>
          </Link>
        </section>
      </main>
      <footer className="mx-auto flex max-w-6xl flex-wrap items-start justify-between gap-8 px-6 py-10">
        <div>
          <Link to="/" className="text-xl font-semibold">
            lifeai.
          </Link>
          <p className="mt-2 text-xs text-ink-500">
            A little more awareness. Every day.
          </p>
        </div>
        <div className="flex gap-6 text-xs text-ink-400">
          <Link to="/login">Sign in</Link>
          <Link to="/privacy">Privacy</Link>
          <a href="/api/openapi.yaml">API</a>
          <a href="https://github.com/biswas-dev/lifeai">Source</a>
        </div>
        <p className="w-full border-t border-ink-800 pt-5 text-[11px] text-ink-500">
          © {new Date().getFullYear()} lifeai · A personal tracking tool.
          Review medical findings and targets with your clinician.
        </p>
      </footer>
    </div>
  );
}
