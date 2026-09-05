import { useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { api } from "../lib/api";
import type { Recipe, RecipeDraft } from "../lib/types";
import { message, useResource } from "../lib/useResource";
import {
  Empty,
  ErrorText,
  Field,
  PageHeader,
  Sheet,
  Spinner,
} from "../components/ui";
import { AuthImage } from "../components/AuthImage";

const blank: RecipeDraft = {
  name: "",
  summary: "",
  minutes: 20,
  servings: 1,
  kcal_per_serving: 0,
  protein_g: 0,
  carbs_g: 0,
  fat_g: 0,
  ingredients: [],
  steps: [],
  tags: [],
  favourite: false,
  photo_id: null,
  source: "manual",
};

export function RecipeEditor({
  recipe,
  onSaved,
  onClose,
}: {
  recipe?: Recipe;
  onSaved: (r: Recipe) => void;
  onClose: () => void;
}) {
  const [draft, setDraft] = useState<RecipeDraft>(recipe || blank);
  const [ingredients, setIngredients] = useState(
    (recipe?.ingredients || []).join("\n"),
  );
  const [steps, setSteps] = useState((recipe?.steps || []).join("\n"));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  return (
    <Sheet
      open
      onClose={onClose}
      title={recipe ? "Edit recipe" : "New recipe"}
      wide
    >
      <form
        className="space-y-4"
        onSubmit={async (e) => {
          e.preventDefault();
          setBusy(true);
          setError("");
          try {
            const body = {
              ...draft,
              ingredients: ingredients.split("\n").filter(Boolean),
              steps: steps.split("\n").filter(Boolean),
            };
            onSaved(
              await (recipe
                ? api.updateRecipe(recipe.id, body)
                : api.createRecipe(body)),
            );
          } catch (e) {
            setError(message(e));
          } finally {
            setBusy(false);
          }
        }}
      >
        <ErrorText>{error}</ErrorText>
        <Field label="Recipe name">
          <input
            className="field"
            required
            value={draft.name}
            onChange={(e) => setDraft({ ...draft, name: e.target.value })}
          />
        </Field>
        <Field label="Description">
          <textarea
            className="field"
            value={draft.summary}
            onChange={(e) => setDraft({ ...draft, summary: e.target.value })}
          />
        </Field>
        <div className="grid grid-cols-2 gap-3">
          {(
            [
              ["minutes", "Prep + cooking minutes"],
              ["servings", "Recipe servings"],
              ["kcal_per_serving", "Calories per serving"],
              ["protein_g", "Protein per serving (g)"],
              ["carbs_g", "Carbs per serving (g)"],
              ["fat_g", "Fat per serving (g)"],
            ] as const
          ).map(([key, label]) => (
            <Field key={key} label={label}>
              <input
                type="number"
                className="field"
                min={key === "servings" ? 1 : 0}
                step="any"
                required
                value={draft[key]}
                onChange={(e) =>
                  setDraft({ ...draft, [key]: Number(e.target.value) })
                }
              />
            </Field>
          ))}
        </div>
        <Field
          label="Ingredients"
          hint="One ingredient per line, including its quantity."
        >
          <textarea
            className="field"
            rows={5}
            value={ingredients}
            onChange={(e) => setIngredients(e.target.value)}
          />
        </Field>
        <Field label="Method" hint="One step per line.">
          <textarea
            className="field"
            rows={5}
            value={steps}
            onChange={(e) => setSteps(e.target.value)}
          />
        </Field>
        <Field label="Tags, separated by commas">
          <input
            className="field"
            value={draft.tags.join(",")}
            onChange={(e) =>
              setDraft({ ...draft, tags: e.target.value.split(",") })
            }
          />
        </Field>
        <button className="btn-primary w-full" disabled={busy}>
          {busy ? "Saving…" : "Save recipe"}
        </button>
      </form>
    </Sheet>
  );
}

export function Recipes() {
  const [query, setQuery] = useState("");
  const [favourite, setFavourite] = useState(false);
  const [edit, setEdit] = useState(false);
  const [paste, setPaste] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const navigate = useNavigate();
  const list = useResource(
    () => api.listRecipes({ q: query, favourite }),
    [query, favourite],
  );
  return (
    <div className="mx-auto max-w-3xl">
      <PageHeader
        title="Your recipe book"
        subtitle="Meals worth making again."
        action={
          <button className="btn-primary" onClick={() => setEdit(true)}>
            New recipe
          </button>
        }
      />
      <div className="mb-5 flex gap-3">
        <input
          className="field flex-1"
          aria-label="Search recipes"
          placeholder="Search recipes or ingredients"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
        <button
          className={favourite ? "btn-primary" : "btn-ghost"}
          aria-pressed={favourite}
          onClick={() => setFavourite(!favourite)}
        >
          Favourites
        </button>
      </div>
      <ErrorText>{list.error || error}</ErrorText>
      {!list.data && !list.error ? (
        <Spinner />
      ) : list.data?.length === 0 ? (
        <Empty>
          Your recipe book is ready for its first meal. Add a favourite recipe
          above.
        </Empty>
      ) : (
        <div className="grid gap-4 sm:grid-cols-2">
          {list.data?.map((r) => (
            <Link
              className="card overflow-hidden transition hover:border-vital-500/40"
              key={r.id}
              to={`/app/recipes/${r.id}`}
            >
              {r.photo_url && (
                <AuthImage
                  src={r.photo_url}
                  alt={r.name}
                  className="h-36 w-full object-cover"
                />
              )}
              <div className="p-5">
                <div className="text-xs text-vital-400">
                  {r.minutes} min · {r.servings} servings {r.favourite && "· ★"}
                </div>
                <h2 className="mt-2 text-lg font-semibold text-ink-100">
                  {r.name}
                </h2>
                <p className="mt-1 line-clamp-2 text-sm text-ink-400">
                  {r.summary}
                </p>
                <div className="mt-4 text-xs text-ink-500">
                  {Math.round(r.kcal_per_serving)} kcal ·{" "}
                  {Math.round(r.protein_g)} g protein / serving
                </div>
              </div>
            </Link>
          ))}
        </div>
      )}
      <details className="card mt-6 p-5">
        <summary className="cursor-pointer font-medium text-ink-200">
          Import a recipe with AI
        </summary>
        <p className="mt-3 text-sm text-ink-400">
          Paste the ingredients and method. Review the resulting recipe and
          estimated nutrition.
        </p>
        <textarea
          aria-label="Recipe text"
          className="field mt-3"
          rows={6}
          value={paste}
          onChange={(e) => setPaste(e.target.value)}
        />
        <button
          disabled={busy || !paste.trim()}
          className="btn-ghost mt-3"
          onClick={async () => {
            setBusy(true);
            setError("");
            try {
              const { recipe } = await api.importRecipe(paste);
              const saved = await api.createRecipe({
                ...recipe,
                source: "import",
              });
              navigate(`/app/recipes/${saved.id}`);
            } catch (e) {
              setError(message(e));
            } finally {
              setBusy(false);
            }
          }}
        >
          {busy ? "Importing…" : "Import recipe"}
        </button>
      </details>
      {edit && (
        <RecipeEditor
          onClose={() => setEdit(false)}
          onSaved={(r) => navigate(`/app/recipes/${r.id}`)}
        />
      )}
    </div>
  );
}
