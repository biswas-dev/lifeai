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

## Sign-in

Google sign-in is configured through the `lifeai-cc` Google Cloud project with callbacks at `https://<environment>/api/auth/google/callback`. Set each deployment's `GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET` secrets. It requests only `openid email profile`, uses browser-bound state and PKCE, and returns the app session in a URL fragment. Verified Gmail and Workspace identities can link to existing accounts; other existing email addresses must use password sign-in. New Google users follow `ALLOW_SIGNUP` without invite codes. The user store and session format remain compatible with go-login. The public privacy page is `/privacy`.

## Account security

Settings → Account security supports authenticator-app TOTP and passkeys through go-login. Enrollment is optional and must be completed by the account owner on their own authenticator or device. TOTP is enabled only after verifying a code, then ten one-use recovery codes are shown for saving. Secrets are encrypted with `ENCRYPTION_KEY`; recovery codes are stored as hashes. Password, Google, and password-reset sign-ins issue a browser-bound MFA challenge instead of a session when TOTP is enabled. Challenges expire after five minutes, allow five attempts, and reject replays. Ten failed factor checks lock the account's code verification for five minutes.

Passkeys require HTTPS (localhost works for development), use `APP_URL` for the exact origin and relying party, and require discoverable credentials plus user verification. Device PIN/biometric verification satisfies MFA without an extra TOTP code. Each environment has separate credentials. Only public keys and authenticator metadata are stored; private keys and biometrics stay with the device/provider. Registration, assertions, origin binding, backup flags, counters, and challenge reuse are covered by signed WebAuthn integration tests.

Security changes require an interactive session authenticated within five minutes, revoke older browser sessions, and reject API/MCP tokens. Existing personal API tokens continue to work within their scopes. A verified session can manage `/api/security/`; public login ceremonies use `/api/auth/mfa/verify` and `/api/auth/passkeys/login/{begin,finish}`. The operator may run `admin reset-2fa <email>` after independently verifying ownership if the owner has lost both their authenticator and recovery codes. This leaves passkeys and password unchanged and revokes browser sessions.

## Daily logging

Today has a six-tile activity grid for water, exercise, meditation, journal, food and photos. Log any duration or amount when it happens; no challenge completion rules apply. Water supports US gallons (default), US fluid ounces, litres and millilitres, with quick additions, custom amounts and undo. The preferred unit is saved to the profile. Drink request IDs prevent retries from counting twice, and manual water totals take precedence over imported readings. Calendar days include water and meditation activity. Mobile uses fixed bottom navigation, a More menu and bottom-sheet forms.

## Data connections

- **75hard:** a one-way pull every 24 hours, restricted by `HARD75_ALLOWED_EMAILS`. The default allows only the owner's account. Connect a 75hard read-only token in Settings; imported rows retain source IDs. Historical photos, food, metrics, workouts, meditation and journal entries are imported. Water readings feed the daily metric, while reading, diet and custom task records appear as clearly labelled 75hard check-ins in the journal. Deleting a source-owned entry in lifeai may be undone by the next source sync.
- **Strava:** connect using OAuth after configuring a Strava app. The callback is `https://<environment>/api/strava/callback`. The scheduler imports activities every 30 minutes. A connection in another application does not automatically authorize a separate OAuth app.
- **Apple Health:** import `export.zip` or `export.xml` in Settings.
- **Samsung Health:** import its data export ZIP in Settings.
- **Phone automation:** POST JSON to `/api/import/health` with a read + write API token. Apple/Samsung uploads are file imports, not native mobile HealthKit/Health Connect apps.

Daily readings resolve by source precedence (manual, Apple, Samsung, webhook, Garmin, Strava, 75hard). Manually edited fields win. Workouts match across sources using compatible activity types, starts within three minutes and comparable durations. Explicitly Strava-derived 75hard copies can also match the same name and exact start when elapsed and moving durations differ. Every matched source ID is retained, so later sync corrections update one workout. Direct Strava details take precedence over its 75hard copy. Distinct IDs from one provider, ambiguous matches and untimed sessions stay separate. Matching is heuristic because 75hard does not expose the original Strava activity ID. Different lab units stay in separate series.

## Blood reports and analysis

Upload PDF/text reports or enter values manually. Dynacare text reports have a deterministic parser; other textual reports can use optional AI extraction. Scanned PDFs without text need manual entry. Always review extracted markers against the original report. The 90-day milestone is a tracking aid, not a prescribed testing schedule. Nutrition estimates and AI suggestions need review.

Blood work charts show the first reading as a baseline, then connect later results using collection dates across years. Key markers lead the page; All markers and search expose every numeric test, with different units kept separate. Red results and clickable “!” flags open the recorded range, an explanation and reading history. HbA1c and ALT include general education linked to MedlinePlus. Each historical reading retains its own reference range; chart shading is explicitly the latest lab range. Flags are not diagnoses or personal treatment targets.

`GET /api/analysis/health` and all `/mcp` tools use stored records without spending model calls. Built-in coaching and meal estimates require a configured AI provider. Visiting the dashboard does not generate a paid coach response.

## MCP

Create a read-only token in Settings → API & MCP access. Configure a streamable HTTP MCP client with:

- URL: `https://lifeai.cc/mcp`
- Header: `Authorization: Bearer <your token>`

Start with `get_health_summary`, then `get_blood_markers`, `list_days`, `get_stats`, recipes or journal tools. `list_photos` and `get_photo` let your agent inspect stored images (thumbnails by default to reduce token usage). Tokens expire and can be revoked in Settings. Write tools require an explicit write scope. `/api/openapi.yaml` describes the REST interface; the MCP tool list is discoverable using `tools/list`.

## Operations and limitations

`/health` and `/api/health` report health. `/api/version` identifies the deployed build. The container includes `/app/admin users`, `reset-password`, `make-admin`, and `reset-75hard`. Self-service email reset is not configured; production account recovery uses the operator CLI. Reset credentials are never written to request logs.

Secrets, personal PDFs, local databases, QA fixtures containing real records, and uploads belong outside Git. The public landing preview and committed tests use fictional data. Back up the database using SQLite's online backup API together with the uploads directory before maintenance.
