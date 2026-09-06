import { useCallback, useEffect, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { api } from "../lib/api";
import type { Day } from "../lib/types";
import { addDays, prettyDate, relativeDay, todayISO } from "../lib/format";
import { DayView } from "../components/DayView";
import { PageHeader, Spinner } from "../components/ui";
import { ChevronIcon } from "../components/Icons";

export function DayDetail() {
  const { date = todayISO() } = useParams();
  const navigate = useNavigate();
  const [day, setDay] = useState<Day | null>(null);
  const [error, setError] = useState("");

  const load = useCallback(
    async (d?: Day) => {
      if (d) {
        setDay(d);
        return;
      }
      try {
        setDay(await api.day(date));
      } catch (e) {
        setError(e instanceof Error ? e.message : "Could not load");
      }
    },
    [date],
  );

  useEffect(() => {
    setDay(null);
    load();
  }, [load]);

  const isFuture = date > todayISO();

  return (
    <div className="mx-auto max-w-3xl">
      <PageHeader
        title={relativeDay(date)}
        stackOnMobile
        subtitle={prettyDate(date, true)}
        action={
          <div className="flex items-center gap-1">
            <button
              className="btn-ghost btn-sm"
              onClick={() => navigate(`/app/day/${addDays(date, -1)}`)}
              aria-label="Previous day"
            >
              <ChevronIcon size={16} className="rotate-180" />
            </button>
            <Link to="/app/history" className="btn-ghost btn-sm">
              Calendar
            </Link>
            <button
              className="btn-ghost btn-sm"
              disabled={isFuture || date === todayISO()}
              onClick={() => navigate(`/app/day/${addDays(date, 1)}`)}
              aria-label="Next day"
            >
              <ChevronIcon size={16} />
            </button>
          </div>
        }
      />
      {error && <p className="text-rose-400">{error}</p>}
      {!day && !error && (
        <div className="flex justify-center py-20">
          <Spinner />
        </div>
      )}
      {day && <DayView day={day} reload={load} />}
    </div>
  );
}
