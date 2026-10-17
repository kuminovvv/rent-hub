# Frontend

Next.js interface for Rent SPB, built with React, TypeScript, Tailwind CSS, shadcn/ui components, and Leaflet. It provides listing search, filters, paginated results, and a map.

For the full architecture and local setup, see the [root README](../README.md).

From PowerShell, after starting the backend on port 8082, set `TELEGRAM_API_KEY` to the same local value configured in `backend/.env`:

```powershell
$env:API_URL = "http://127.0.0.1:8082"
$env:TELEGRAM_API_KEY = "<same local value as backend/.env>"
pnpm install --frozen-lockfile
pnpm dev --hostname 127.0.0.1
```
