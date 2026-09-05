# lifeai

A personal health journal for food, recipes, training, meditation, body metrics, blood work and reflections. React + TypeScript, Go, SQLite, shared go-login / go-ai / go-photo / go-api libraries. One Go service serves the API and compiled frontend.

## Local development

Requires Go 1.26, Node 24 and Poppler (`pdftotext`) for PDF extraction.

```sh
cd web
npm ci
npm run build
cd ../api
FRONTEND_DIST=../web/dist go run ./cmd/api
```

Open http://localhost:8088. For Vite hot reload, run `npm run dev` in `web`; its API proxy targets port 8088. Environment options are listed in `.env.example` and `api/internal/config/config.go`. Production requires distinct JWT, OAuth state and integration encryption secrets.

## Checks

```sh
cd api && go test ./... && go vet ./...
cd ../web && npm ci && npm test && npm run build
```

## Deployment

`main` deploys staging.lifeai.cc, `uat` deploys uat.lifeai.cc, and `production` deploys lifeai.cc. Both test jobs must pass before a deployment starts. Pull requests use GitHub-hosted test runners; deployment uses the owner's existing runners and hosts. Host nginx terminates TLS and connects to loopback port 13440. SQLite and uploads live outside the source directory in `data/` and survive redeploys.

`deployment/scripts/deploy.sh` documents required GitHub secrets. `SSH_KNOWN_HOSTS` pins the target host keys. It verifies container health and origin TLS; a separate GitHub-hosted job verifies the public Cloudflare endpoint and exact deployed commit from outside the origin network. For manual deployments, supply the same secrets as environment variables and run `bash deployment/scripts/deploy.sh staging` (or `uat` / `production`), then check the public endpoint. Deployments archive the committed HEAD.

## Data connections

- **75hard:** a one-way pull every 24 hours, restricted by `HARD75_ALLOWED_EMAILS`. The default allows only the owner's account. Connect a 75hard read-only token in Settings; imported rows retain source IDs. Historical photos, food, metrics, workouts, meditation and journal entries are imported. Water readings feed the daily metric, while reading, diet and custom task records appear as clearly labelled 75hard check-ins in the journal. Deleting a source-owned entry in lifeai may be undone by the next source sync.
- **Strava:** connect using OAuth after configuring a Strava app. The callback is `https://<environment>/api/strava/callback`. The scheduler imports activities every 30 minutes. A connection in another application does not automatically authorize a separate OAuth app.
- **Apple Health:** import `export.zip` or `export.xml` in Settings.
- **Samsung Health:** import its data export ZIP in Settings.
- **Phone automation:** POST JSON to `/api/import/health` with a read + write API token. Apple/Samsung uploads are file imports, not native mobile HealthKit/Health Connect apps.

Daily readings resolve by source precedence (manual, Apple, Samsung, webhook, Garmin, Strava, 75hard). Manually edited fields win. Workouts with starts within three minutes and comparable durations are treated as the same activity; unmatched or untimed sessions are retained. This is heuristic matching, not an assertion that all vendor exports are lossless. Different lab units stay in separate series.

## Blood reports and analysis

Upload PDF/text reports or enter values manually. Dynacare text reports have a deterministic parser; other textual reports can use optional AI extraction. Scanned PDFs without text need manual entry. Always review extracted markers against the original report. The 90-day milestone is a tracking aid, not a prescribed testing schedule. Nutrition estimates and AI suggestions need review.

`GET /api/analysis/health` and all `/mcp` tools use stored records without spending model calls. Built-in coaching and meal estimates require a configured AI provider. Visiting the dashboard does not generate a paid coach response.

## MCP

Create a read-only token in Settings → API & MCP access. Configure a streamable HTTP MCP client with:

- URL: `https://lifeai.cc/mcp`
- Header: `Authorization: Bearer <your token>`

Start with `get_health_summary`, then `get_blood_markers`, `list_days`, `get_stats`, recipes or journal tools. `list_photos` and `get_photo` let your agent inspect stored images (thumbnails by default to reduce token usage). Tokens expire and can be revoked in Settings. Write tools require an explicit write scope. `/api/openapi.yaml` describes the REST interface; the MCP tool list is discoverable using `tools/list`.

## Operations and limitations

`/health` and `/api/health` report health. `/api/version` identifies the deployed build. The container includes `/app/admin users`, `reset-password`, `make-admin`, and `reset-75hard`. Self-service email reset is not configured; production account recovery uses the operator CLI. Reset credentials are never written to request logs.

Secrets, personal PDFs, local databases, QA fixtures containing real records, and uploads belong outside Git. The public landing preview and committed tests use fictional data. Back up the database using SQLite's online backup API together with the uploads directory before maintenance.
