# Rent Hub

An experimental full-stack rental listing catalog for Saint Petersburg. Rent Hub collects posts from configured Telegram channels, structures listing details, enriches them with geocoding and building data, and presents them in a searchable web interface with a map.

## What it includes

- Search and filters for rental listings
- Map and paginated listing results
- A Go API backed by PostgreSQL
- Telegram ingestion and AI-assisted structuring workers
- Geocoding and building-catalog enrichment

## Stack

- **Web:** Next.js, React, TypeScript, Tailwind CSS, Leaflet
- **API and workers:** Go
- **Storage:** PostgreSQL
- **Integrations:** Telegram, Qwen, Geoapify

## Repository layout

```text
frontend/   Next.js application
backend/    Go API, ingestion workers, and SQL migrations
```

## Local development

You need Go, PostgreSQL, Node.js, and pnpm. External integrations require your own credentials. The repository does not include a database dump, Telegram session, media files, or production listings.

1. Copy `backend/.env.example` to `backend/.env` and fill in the values for your local PostgreSQL instance and any integrations you plan to use.
2. Apply the SQL files in `backend/migrations/` in filename order to an empty database. For example, from a Bash shell at the repository root:

   ```bash
   set -a
   source backend/.env
   set +a
   for migration in backend/migrations/*.sql; do
     psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f "$migration"
   done
   ```

3. Start the API from the repository root:

   ```bash
   cd backend
   set -a
   source .env
   set +a
   go run ./cmd/api
   ```

   The API listens on `:8082` by default.

4. Start the frontend in another terminal:

   ```bash
   cd frontend
   export TELEGRAM_API_KEY="the same local value configured in backend/.env"
   API_URL=http://127.0.0.1:8082 pnpm install --frozen-lockfile
   API_URL=http://127.0.0.1:8082 pnpm dev --hostname 127.0.0.1
   ```

   The Next.js server needs the same `TELEGRAM_API_KEY` as the backend for its server-side API requests. Open `http://127.0.0.1:3000`.

Telegram ingestion is a separate worker. To configure it, copy `backend/telegram_sources.example.txt` to the ignored local file `backend/telegram_sources.txt`, add channels you are authorized to ingest, and provide Telegram credentials in `backend/.env`. Do not commit the local source list, session, media, database exports, or real listing data.

## Data and privacy

Listings can contain precise addresses, coordinates, raw post text, photos, and contact details. Use only data you have the right to process. Keep real listings, Telegram session files, media, database dumps, logs, and credentials out of Git. Use synthetic data in screenshots and demonstrations.

This repository is a source-code demonstration; it does not include a hosted demo or production dataset.
