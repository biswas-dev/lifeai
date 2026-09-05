import { useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { api } from "../lib/api";
import { todayISO } from "../lib/format";
import { message, useResource } from "../lib/useResource";
import { ErrorText, Field, PageHeader, Spinner } from "../components/ui";
import { RecipeEditor } from "./Recipes";

export function RecipeDetail() {
  const { id } = useParams();
  const navigate = useNavigate();
  const resource = useResource(() => api.recipe(Number(id)), [id]);
  const [edit, setEdit] = useState(false);
  const [servings, setServings] = useState(1);
  const [date, setDate] = useState(todayISO());
  const [slot, setSlot] = useState("dinner");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [saved, setSaved] = useState(false);
  const r = resource.data;
  async function act(fn: () => Promise<unknown>) {
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
  return (
    <div className="mx-auto max-w-3xl">
      <Link className="text-sm text-vital-400" to="/app/recipes">
        ← Recipes
      </Link>
      <ErrorText>{resource.error || error}</ErrorText>
      {!r ? (
        !resource.error && <Spinner />
      ) : (
        <>
          <div className="mt-4">
            <PageHeader
              title={r.name}
              subtitle={`${r.minutes} minutes · makes ${r.servings} servings`}
              action={
                <button className="btn-ghost" onClick={() => setEdit(true)}>
                  Edit
                </button>
              }
            />
          </div>
          <p className="text-ink-400">{r.summary}</p>
          <div className="my-5 flex flex-wrap gap-2">
            {[
              `${r.kcal_per_serving} kcal`,
              `${r.protein_g} g protein`,
              `${r.carbs_g} g carbs`,
              `${r.fat_g} g fat`,
            ].map((v) => (
              <span key={v} className="chip">
                {v} / serving
              </span>
            ))}
          </div>
          <div className="grid gap-5 md:grid-cols-2">
            <section className="card p-5">
              <h2 className="mb-3 font-semibold">Ingredients</h2>
              <ul className="space-y-2 text-sm text-ink-300">
                {r.ingredients.map((line, i) => (
                  <li key={i}>• {line}</li>
                ))}
              </ul>
            </section>
            <section className="card p-5">
              <h2 className="mb-3 font-semibold">Method</h2>
              <ol className="list-decimal space-y-3 pl-5 text-sm text-ink-300">
                {r.steps.map((line, i) => (
                  <li key={i}>{line}</li>
                ))}
              </ol>
            </section>
          </div>
          <form
            className="card mt-5 space-y-4 p-5"
            onSubmit={(e) => {
              e.preventDefault();
              void act(async () => {
                await api.cookRecipe(r.id, { date, slot, servings });
                setSaved(true);
                resource.reload();
              });
            }}
          >
            <h2 className="font-semibold">Cook & log</h2>
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
              <Field label="Date">
                <input
                  type="date"
                  className="field"
                  required
                  value={date}
                  onChange={(e) => {
                    setDate(e.target.value);
                    setSaved(false);
                  }}
                />
              </Field>
              <Field label="Meal">
                <select
                  className="field"
                  value={slot}
                  onChange={(e) => setSlot(e.target.value)}
                >
                  {["breakfast", "lunch", "dinner", "snack"].map((s) => (
                    <option key={s}>{s}</option>
                  ))}
                </select>
              </Field>
              <Field label="Servings eaten">
                <input
                  className="field"
                  type="number"
                  min="0.1"
                  step="0.1"
                  value={servings}
                  onChange={(e) => setServings(Number(e.target.value))}
                  required
                />
              </Field>
            </div>
            <button className="btn-primary" disabled={busy || saved}>
              {busy ? "Logging…" : saved ? "Meal logged" : "Log this meal"}
            </button>
            {saved && (
              <Link
                to={`/app/day/${date}`}
                className="ml-3 text-sm text-vital-400"
              >
                View day →
              </Link>
            )}
          </form>
          <div className="mt-5 flex justify-between">
            <button
              className="btn-ghost"
              disabled={busy}
              onClick={() =>
                void act(async () =>
                  resource.setData(
                    await api.updateRecipe(r.id, { favourite: !r.favourite }),
                  ),
                )
              }
            >
              {r.favourite ? "★ Favourite" : "☆ Add favourite"}
            </button>
            <button
              className="btn-ghost text-rose-400"
              disabled={busy}
              onClick={() => {
                if (
                  window.confirm(
                    "Delete this recipe? Previously logged meals will remain.",
                  )
                )
                  void act(async () => {
                    await api.deleteRecipe(r.id);
                    navigate("/app/recipes");
                  });
              }}
            >
              Delete recipe
            </button>
          </div>
          {edit && (
            <RecipeEditor
              recipe={r}
              onClose={() => setEdit(false)}
              onSaved={(v) => {
                resource.setData(v);
                setEdit(false);
              }}
            />
          )}
        </>
      )}
    </div>
  );
}
