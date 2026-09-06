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
import { BloodTrend } from "../components/BloodTrend";
import {
  BloodDetails,
  BloodFlag,
  type BloodDetailSelection,
} from "../components/BloodDetails";
import {
  isFlagged,
  orderedPoints,
  referenceRange,
  resultFlag,
  resultValue,
  seriesResult,
} from "../lib/blood";

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
  const [detail, setDetail] = useState<BloodDetailSelection | null>(null);
  const [filter, setFilter] = useState("key");
  const [search, setSearch] = useState("");
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
  const allMarkers = (markers.data || []).map((m) => ({
    ...m,
    points: orderedPoints(m.points),
  }));
  const flagged = allMarkers.filter((m) =>
    isFlagged(resultFlag(seriesResult(m))),
  );
  const keyMarkers = allMarkers.filter(
    (m) => m.watch || isFlagged(resultFlag(seriesResult(m))),
  );
  const visibleMarkers = (
    search.trim() || filter === "all"
      ? allMarkers
      : filter === "flagged"
        ? flagged
        : keyMarkers
  )
    .filter((m) =>
      `${m.name} ${m.code} ${m.category}`
        .toLowerCase()
        .includes(search.trim().toLowerCase()),
    )
    .sort(
      (a, b) =>
        Number(isFlagged(resultFlag(seriesResult(b)))) -
        Number(isFlagged(resultFlag(seriesResult(a)))),
    );
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
      {allMarkers.length > 0 && (
        <section className="mb-6" aria-labelledby="blood-trends-heading">
          <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
            <h2 id="blood-trends-heading" className="font-semibold">
              Marker trends
            </h2>
            <span className="text-xs text-ink-500">
              {reports.data?.length ?? "—"} report
              {reports.data?.length === 1 ? "" : "s"} · {allMarkers.length}{" "}
              markers tracked
            </span>
          </div>
          {flagged.length > 0 && (
            <div className="mb-4 rounded-xl border border-rose-500/25 bg-rose-500/5 p-4 text-sm text-rose-400">
              <p className="font-semibold">
                {flagged.length} latest result{flagged.length === 1 ? "" : "s"}{" "}
                flagged
              </p>
              <p className="mt-1">
                {flagged.map((m) => m.name).join(", ")}. Select a red ! to see
                why.
              </p>
            </div>
          )}
          <p className="mb-3 text-sm text-ink-400">
            Each new report adds to your history. Matching markers and units are
            compared across collection dates, including future years.
          </p>
          <div className="mb-4 space-y-3">
            <div
              className="flex flex-wrap gap-2"
              aria-label="Filter marker trends"
            >
              {[
                ["key", `Key markers (${keyMarkers.length})`],
                ["all", `All markers (${allMarkers.length})`],
                ["flagged", `Flagged (${flagged.length})`],
              ].map(([value, label]) => (
                <button
                  key={value}
                  className={`chip min-h-11 ${filter === value && !search.trim() ? "chip-active" : ""}`}
                  aria-pressed={filter === value && !search.trim()}
                  onClick={() => {
                    setFilter(value);
                    setSearch("");
                  }}
                >
                  {label}
                </button>
              ))}
            </div>
            <input
              type="search"
              className="field"
              aria-label="Search all blood markers"
              placeholder="Find a marker…"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
          </div>
          <div className="grid gap-3 sm:grid-cols-2">
            {visibleMarkers.map((m) => {
              const result = seriesResult(m),
                abnormal = isFlagged(resultFlag(result));
              const open = () => setDetail({ result, series: m });
              const change =
                m.points.length > 1
                  ? m.points[m.points.length - 1].value - m.points[0].value
                  : null;
              return (
                <section
                  key={`${m.code}|${m.unit}`}
                  aria-label={`${m.name} trend`}
                  className={`card min-w-0 p-4 ${abnormal ? "border-rose-500/30" : ""}`}
                >
                  <div className="mb-1 flex items-center justify-between gap-2">
                    <h3
                      className={`text-sm font-semibold ${abnormal ? "text-rose-400" : ""}`}
                    >
                      {m.name}
                    </h3>
                    <BloodFlag result={result} onClick={open} />
                  </div>
                  <p
                    className={`text-2xl font-semibold tabular-nums ${abnormal ? "text-rose-400" : "text-ink-100"}`}
                  >
                    {resultValue(result)}
                  </p>
                  <p className="mb-3 mt-1 text-xs text-ink-500">
                    {result.date && prettyDate(result.date, true)} · Latest
                  </p>
                  <BloodTrend series={m} />
                  <p className="mt-3 text-xs text-ink-400">
                    Lab reference: {referenceRange(result)}
                  </p>
                  {change != null && (
                    <p className="mt-1 text-xs text-ink-400">
                      {change > 0 ? "+" : ""}
                      {Number(change.toPrecision(6))}{" "}
                      {m.unit === "%" ? "percentage points" : m.unit} since
                      first reading
                    </p>
                  )}
                  <button
                    className="mt-3 min-h-11 text-sm font-medium text-vital-500"
                    onClick={open}
                    aria-label={`View ${m.name} history`}
                  >
                    View history →
                  </button>
                </section>
              );
            })}
          </div>
          {visibleMarkers.length === 0 && (
            <Empty>
              {search
                ? "No markers match your search."
                : filter === "flagged"
                  ? "No latest numeric results are flagged."
                  : "Choose All markers to explore your results."}
            </Empty>
          )}
        </section>
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
                {r.counts?.abnormal > 0 && (
                  <p className="mt-2 text-xs font-semibold text-rose-400">
                    ! {r.counts.abnormal} flagged result
                    {r.counts.abnormal === 1 ? "" : "s"}
                  </p>
                )}
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
      {detail && (
        <BloodDetails selection={detail} onClose={() => setDetail(null)} />
      )}
      {report && !detail && (
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
                    {report.markers.map((m, i) => {
                      const result = { ...m, date: report.taken_on };
                      const abnormal = isFlagged(resultFlag(result));
                      return (
                        <tr
                          key={i}
                          className={`border-t border-ink-800 ${abnormal ? "bg-rose-500/5 text-rose-400" : ""}`}
                        >
                          <td className="py-3 pr-3">{m.name}</td>
                          <td
                            className={`pr-3 tabular-nums ${abnormal ? "font-semibold" : ""}`}
                          >
                            {resultValue(result)}
                          </td>
                          <td className="pr-3 text-xs text-ink-400">
                            {referenceRange(result)}
                          </td>
                          <td>
                            <BloodFlag
                              result={result}
                              onClick={() =>
                                setDetail({
                                  result,
                                  series: allMarkers.find(
                                    (s) =>
                                      s.code ===
                                        (m.code ||
                                          `custom:${m.name.trim().toLowerCase()}`) &&
                                      s.unit === m.unit,
                                  ),
                                })
                              }
                            />
                          </td>
                        </tr>
                      );
                    })}
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
