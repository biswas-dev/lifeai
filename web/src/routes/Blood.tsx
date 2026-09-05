import { useRef, useState } from "react";
import { api } from "../lib/api";
import type { BloodMarker, BloodReport } from "../lib/types";
import { todayISO, prettyDate } from "../lib/format";
import { message, useResource } from "../lib/useResource";
import {
  Empty,
  ErrorText,
  Field,
  PageHeader,
  Sheet,
  Spinner,
} from "../components/ui";
import { Sparkline } from "../components/Sparkline";

const blankMarker = (): BloodMarker => ({
  id: 0,
  code: "",
  name: "",
  category: "",
  value: null,
  value_text: "",
  unit: "",
  ref_low: null,
  ref_high: null,
  ref_text: "",
  flag: "",
  watch: false,
});

export function Blood() {
  const reports = useResource(() => api.bloodReports());
  const markers = useResource(() => api.bloodMarkers());
  const [report, setReport] = useState<BloodReport | null>(null);
  const [edit, setEdit] = useState(false);
  const [date, setDate] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const input = useRef<HTMLInputElement>(null);
  function reload() {
    reports.reload();
    markers.reload();
  }
  async function action(fn: () => Promise<void>) {
    setBusy(true);
    setError("");
    try {
      await fn();
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy(false);
    }
  }
  async function download() {
    if (!report?.file_url) return;
    const url = await api.photoObjectURL(report.file_url);
    const a = document.createElement("a");
    a.href = url;
    a.download = report.file_name || "report.pdf";
    a.click();
    setTimeout(() => URL.revokeObjectURL(url), 60000);
  }
  const watched =
    markers.data?.filter(
      (m) => m.watch || ["high", "low", "abnormal"].includes(m.flag),
    ) || [];
  return (
    <div className="mx-auto max-w-3xl">
      <PageHeader
        title="Blood work"
        subtitle="Keep the source. Review the values. Follow the trend."
      />
      <div className="card mb-5 space-y-3 p-5">
        <h2 className="font-semibold">Add a lab report</h2>
        <p className="text-sm text-ink-400">
          Upload a PDF or text report, then check the extracted values against
          your original. Scanned PDFs may need manual entry.
        </p>
        <div className="flex flex-wrap items-end gap-3">
          <Field
            label="Collection date"
            hint="Leave blank to use the date found in the report."
          >
            <input
              className="field"
              type="date"
              value={date}
              onChange={(e) => setDate(e.target.value)}
            />
          </Field>
          <button
            className="btn-primary"
            disabled={busy}
            onClick={() => input.current?.click()}
          >
            {busy ? "Processing…" : "Upload report"}
          </button>
          <button
            className="btn-ghost"
            disabled={busy}
            onClick={() => {
              setError("");
              setReport({
                id: 0,
                taken_on: date || todayISO(),
                lab: "",
                notes: "",
                ordered_by: "",
                has_file: false,
                parse_status: "manual",
                markers: [blankMarker()],
                counts: {},
                created_at: "",
              });
              setEdit(true);
            }}
          >
            Enter manually
          </button>
        </div>
        <input
          ref={input}
          type="file"
          accept=".pdf,.txt"
          className="hidden"
          onChange={(e) => {
            const file = e.target.files?.[0];
            if (file)
              void action(async () => {
                setReport(await api.uploadBlood(file, date));
                setEdit(false);
                reload();
                if (input.current) input.current.value = "";
              });
          }}
        />
      </div>
      <ErrorText>{error || reports.error || markers.error}</ErrorText>
      {watched.length > 0 && (
        <div className="mb-6 grid gap-3 sm:grid-cols-2">
          {watched.map((m) => (
            <section key={`${m.code}|${m.unit}`} className="card p-5">
              <div className="mb-3 flex items-start justify-between">
                <h2 className="text-sm font-medium">{m.name}</h2>
                <span
                  className={`text-xs ${["high", "low", "abnormal"].includes(m.flag) ? "text-rose-400" : "text-ink-500"}`}
                >
                  {m.flag || "No range"}
                </span>
              </div>
              <Sparkline
                points={m.points}
                unit={m.unit}
                refLow={m.ref_low}
                refHigh={m.ref_high}
                height={80}
              />
              <p className="mt-2 text-xs text-ink-500">
                {m.latest?.date} · {m.points.length} reading
                {m.points.length === 1 ? "" : "s"}
              </p>
            </section>
          ))}
        </div>
      )}
      <h2 className="mb-3 font-semibold">Your reports</h2>
      {!reports.data && !reports.error ? (
        <Spinner />
      ) : reports.data?.length === 0 ? (
        <Empty>
          Your first report becomes the baseline. Add your next report to
          compare results.
        </Empty>
      ) : (
        <div className="space-y-3">
          {reports.data?.map((r) => (
            <button
              className="card flex w-full items-center justify-between gap-3 p-5 text-left"
              key={r.id}
              disabled={busy}
              onClick={() =>
                void action(async () => {
                  setReport(await api.bloodReport(r.id));
                  setEdit(false);
                })
              }
            >
              <div>
                <h3 className="font-medium text-ink-100">
                  {prettyDate(r.taken_on, true)}
                </h3>
                <p className="mt-1 text-xs text-ink-500">
                  {r.lab || "Lab report"} · {r.file_name || "Manual entry"}
                </p>
              </div>
              <span className="text-xs text-ink-400">
                {r.parse_status === "failed"
                  ? "Needs review"
                  : `${r.counts?.total || 0} markers`}{" "}
                →
              </span>
            </button>
          ))}
        </div>
      )}
      <p className="mt-5 text-xs leading-relaxed text-ink-500">
        Flags use the report’s reference ranges and are not a diagnosis. Lab
        targets and retest timing should be agreed with your clinician.
      </p>
      {report && (
        <Sheet
          open
          wide
          title={report.id ? `Report · ${report.taken_on}` : "New lab report"}
          onClose={() => setReport(null)}
        >
          <div className="space-y-4">
            <ErrorText>{error || report.parse_error}</ErrorText>
            <div className="flex flex-wrap gap-2">
              {report.has_file && (
                <button
                  className="btn-ghost btn-sm"
                  disabled={busy}
                  onClick={() => void action(download)}
                >
                  Download original
                </button>
              )}
              <button
                className="btn-ghost btn-sm"
                onClick={() => setEdit(!edit)}
              >
                {edit ? "Preview" : "Review / correct values"}
              </button>
            </div>
            {edit ? (
              <>
                <Field label="Collection date">
                  <input
                    type="date"
                    className="field"
                    value={report.taken_on}
                    onChange={(e) =>
                      setReport({ ...report, taken_on: e.target.value })
                    }
                  />
                </Field>
                <Field label="Laboratory">
                  <input
                    className="field"
                    value={report.lab}
                    onChange={(e) =>
                      setReport({ ...report, lab: e.target.value })
                    }
                  />
                </Field>
                <Field label="Notes">
                  <textarea
                    className="field"
                    value={report.notes}
                    onChange={(e) =>
                      setReport({ ...report, notes: e.target.value })
                    }
                  />
                </Field>
                {report.markers.map((m, i) => {
                  const patch = (p: Partial<BloodMarker>) =>
                    setReport({
                      ...report,
                      markers: report.markers.map((v, n) =>
                        n === i ? { ...v, ...p } : v,
                      ),
                    });
                  return (
                    <div
                      className="rounded-xl border border-ink-700 p-3"
                      key={i}
                    >
                      <div className="grid grid-cols-2 gap-2">
                        <Field label="Marker name">
                          <input
                            className="field"
                            value={m.name}
                            onChange={(e) => patch({ name: e.target.value })}
                          />
                        </Field>
                        <Field label="Marker code" hint="e.g. hba1c, ldl, alt">
                          <input
                            className="field"
                            value={m.code}
                            onChange={(e) => patch({ code: e.target.value })}
                          />
                        </Field>
                        <Field label="Value">
                          <input
                            className="field"
                            type="number"
                            step="any"
                            value={m.value ?? ""}
                            onChange={(e) =>
                              patch({
                                value:
                                  e.target.value === ""
                                    ? null
                                    : Number(e.target.value),
                              })
                            }
                          />
                        </Field>
                        <Field label="Unit">
                          <input
                            className="field"
                            value={m.unit}
                            onChange={(e) => patch({ unit: e.target.value })}
                          />
                        </Field>
                        <Field label="Reference low">
                          <input
                            className="field"
                            type="number"
                            step="any"
                            value={m.ref_low ?? ""}
                            onChange={(e) =>
                              patch({
                                ref_low:
                                  e.target.value === ""
                                    ? null
                                    : Number(e.target.value),
                              })
                            }
                          />
                        </Field>
                        <Field label="Reference high">
                          <input
                            className="field"
                            type="number"
                            step="any"
                            value={m.ref_high ?? ""}
                            onChange={(e) =>
                              patch({
                                ref_high:
                                  e.target.value === ""
                                    ? null
                                    : Number(e.target.value),
                              })
                            }
                          />
                        </Field>
                        <Field label="Text result">
                          <input
                            className="field"
                            value={m.value_text}
                            onChange={(e) =>
                              patch({ value_text: e.target.value })
                            }
                          />
                        </Field>
                        <Field label="Lab flag">
                          <select
                            className="field"
                            value={m.flag}
                            onChange={(e) => patch({ flag: e.target.value })}
                          >
                            {[
                              "",
                              "normal",
                              "high",
                              "low",
                              "abnormal",
                              "see_details",
                            ].map((f) => (
                              <option key={f} value={f}>
                                {f || "Automatic"}
                              </option>
                            ))}
                          </select>
                        </Field>
                      </div>
                      <button
                        className="mt-2 text-xs text-rose-400"
                        onClick={() =>
                          setReport({
                            ...report,
                            markers: report.markers.filter((_, n) => n !== i),
                          })
                        }
                      >
                        Remove marker
                      </button>
                    </div>
                  );
                })}
                <button
                  className="btn-ghost"
                  onClick={() =>
                    setReport({
                      ...report,
                      markers: [...report.markers, blankMarker()],
                    })
                  }
                >
                  Add marker
                </button>
              </>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full text-left text-sm">
                  <thead className="text-xs text-ink-500">
                    <tr>
                      <th className="py-3">Marker</th>
                      <th>Result</th>
                      <th>Lab range</th>
                      <th>Flag</th>
                    </tr>
                  </thead>
                  <tbody>
                    {report.markers.map((m, i) => (
                      <tr key={i} className="border-t border-ink-800">
                        <td className="py-3 pr-3">{m.name}</td>
                        <td className="pr-3 tabular-nums">
                          {m.value ?? m.value_text} {m.unit}
                        </td>
                        <td className="pr-3 text-xs text-ink-400">
                          {m.ref_text ||
                            `${m.ref_low ?? "—"} – ${m.ref_high ?? "—"}`}
                        </td>
                        <td
                          className={
                            ["high", "low", "abnormal"].includes(m.flag)
                              ? "text-rose-400"
                              : "text-ink-500"
                          }
                        >
                          {m.flag}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
                {report.markers.length === 0 && (
                  <Empty>
                    No markers extracted. Use “Review / correct values” to add
                    them.
                  </Empty>
                )}
              </div>
            )}
            <div className="flex justify-between">
              {edit && (
                <button
                  className="btn-primary"
                  disabled={
                    busy ||
                    !report.taken_on ||
                    report.markers.some((m) => !m.name.trim())
                  }
                  onClick={() =>
                    void action(async () => {
                      const body = {
                        taken_on: report.taken_on,
                        lab: report.lab,
                        notes: report.notes,
                        markers: report.markers,
                      };
                      setReport(
                        await (report.id
                          ? api.updateBlood(report.id, body)
                          : api.createBlood(body)),
                      );
                      setEdit(false);
                      reload();
                    })
                  }
                >
                  Save report
                </button>
              )}
              {report.id > 0 && (
                <button
                  className="btn-danger ml-auto"
                  disabled={busy}
                  onClick={() => {
                    if (
                      window.confirm(
                        "Delete this report and its original file?",
                      )
                    )
                      void action(async () => {
                        await api.deleteBlood(report.id);
                        setReport(null);
                        reload();
                      });
                  }}
                >
                  Delete report
                </button>
              )}
            </div>
          </div>
        </Sheet>
      )}
    </div>
  );
}
