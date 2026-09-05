import { useState } from "react";
import { api } from "../lib/api";
import { message, useResource } from "../lib/useResource";
import type { JournalEntry } from "../lib/types";
import { prettyDate, todayISO } from "../lib/format";
import {
  Empty,
  ErrorText,
  Field,
  PageHeader,
  Sheet,
  Spinner,
} from "../components/ui";

export function Journal() {
  const [query, setQuery] = useState("");
  const list = useResource(() => api.journal(query), [query]);
  const [draft, setDraft] = useState<Partial<JournalEntry> | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  return (
    <div className="mx-auto max-w-3xl">
      <PageHeader
        title="A little room to reflect"
        subtitle="The story behind your numbers."
        action={
          <button
            className="btn-primary"
            onClick={() => {
              setDraft({ title: "", body: "", date: todayISO() });
              setError("");
            }}
          >
            Write
          </button>
        }
      />
      <input
        className="field mb-5"
        aria-label="Search journal"
        placeholder="Search your journal"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
      />
      <ErrorText>{list.error}</ErrorText>
      {!list.data && !list.error ? (
        <Spinner />
      ) : list.data?.length === 0 ? (
        <Empty>
          What went well today? What would you like to change tomorrow?
        </Empty>
      ) : (
        <div className="space-y-3">
          {list.data?.map((j) => (
            <button
              key={j.id}
              className="card block w-full p-5 text-left"
              onClick={() => {
                setDraft(j);
                setError("");
              }}
            >
              <p className="text-xs text-vital-400">
                {prettyDate(j.date, true)} · {j.source}
              </p>
              <h2 className="mt-2 font-semibold text-ink-100">
                {j.title || "Daily reflection"}
              </h2>
              <p className="mt-2 whitespace-pre-wrap text-sm text-ink-400">
                {j.body || j.snippet}
              </p>
            </button>
          ))}
        </div>
      )}
      {draft && (
        <Sheet
          open
          wide
          title={draft.id ? "Edit reflection" : "New reflection"}
          onClose={() => setDraft(null)}
        >
          <form
            className="space-y-4"
            onSubmit={async (e) => {
              e.preventDefault();
              setBusy(true);
              setError("");
              try {
                const body = {
                  title: draft.title || "",
                  body: draft.body || "",
                  date: draft.date,
                };
                if (draft.id) await api.updateJournal(draft.id, body);
                else await api.createJournal(body);
                setDraft(null);
                list.reload();
              } catch (e) {
                setError(message(e));
              } finally {
                setBusy(false);
              }
            }}
          >
            <ErrorText>{error}</ErrorText>
            <Field label="Date">
              <input
                className="field"
                type="date"
                required
                value={draft.date}
                disabled={!!draft.id}
                onChange={(e) => setDraft({ ...draft, date: e.target.value })}
              />
            </Field>
            <Field label="Title">
              <input
                className="field"
                value={draft.title}
                onChange={(e) => setDraft({ ...draft, title: e.target.value })}
              />
            </Field>
            <Field label="Reflection">
              <textarea
                className="field"
                rows={9}
                required
                value={draft.body}
                onChange={(e) => setDraft({ ...draft, body: e.target.value })}
              />
            </Field>
            <div className="flex justify-between">
              <button className="btn-primary" disabled={busy}>
                Save reflection
              </button>
              {draft.id && (
                <button
                  type="button"
                  className="btn-danger"
                  disabled={busy}
                  onClick={async () => {
                    if (!window.confirm("Delete this journal entry?")) return;
                    setBusy(true);
                    try {
                      await api.deleteJournal(draft.id!);
                      setDraft(null);
                      list.reload();
                    } catch (e) {
                      setError(message(e));
                    } finally {
                      setBusy(false);
                    }
                  }}
                >
                  Delete
                </button>
              )}
            </div>
          </form>
        </Sheet>
      )}
    </div>
  );
}
