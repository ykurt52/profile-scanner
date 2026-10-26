# SkillScanner

A portable Windows tool that reports which languages a GitHub profile uses, for what purpose and in which sector.

## Usage

1. Put `SkillScanner.exe` in any folder and run it (no install needed).
2. Enter a username. A GitHub token and an Anthropic API key are optional.
3. Click **Precheck**. It shows how many requests the scan will spend and how much quota you have left.
4. Click **Start scan**. The report is saved to `reports/` as HTML and JSON and opens in the browser.

All data stays next to the exe: `skillscanner.config.json`, `cache/`, `reports/`, `data/`.

## GitHub quota and ban protection

- Only the official REST API is used; no HTML scraping.
- Requests are sent one by one with at least 250 ms between them. No parallel requests.
- A cost estimate is shown and confirmed before each scan.
- 20% of the quota is always kept in reserve. When the remaining quota reaches it, the scan stops and a partial report is produced.
- Each scan has a request budget (default 300). At most 5 requests are spent per repo.
- An ETag cache is used. For unchanged data GitHub returns `304`, which is free when a token is used.
- On `403`/`429` (rate limit) the scan stops without retrying.
- A read-only fine-grained token is recommended ("Public repositories (read-only)").
- When "Remember" is checked, keys are encrypted with Windows DPAPI. Only the same Windows user can decrypt them.

## How the analysis works

- **Language:** GitHub byte counts. Repos are weighted by recency and popularity. Forks are skipped.
- **Purpose:** manifest files (`package.json`, `requirements.txt`, `platformio.ini`, ...), file layout, languages and keywords. Rules are in `internal/analyze/rules.go`.
- **Sector:** keywords in descriptions, topics and READMEs. The keyword lists contain Turkish words too, so Turkish READMEs are matched.
- **Profile README:** the `user/user` repo is not counted as a project.
- **Optional Claude interpretation:** a digest of public repos is sent to `claude-opus-5-5` in one request. The GitHub token is never sent.

## Development

```bash
go test ./...
```

Tests use a fake GitHub server and never connect to the real GitHub.

```bash
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -H windowsgui" -o dist/SkillScanner.exe .
```
