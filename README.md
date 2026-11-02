# vrotrss

A fast terminal UI for [FreshRSS](https://freshrss.org/), written in Go. Run it as
`vrot`. It looks and behaves like the FreshRSS web interface (folders and feeds in a
left sidebar, articles on the right) and stays in sync with it: read, unread and
favourite state is stored on your FreshRSS server, so what you do in the terminal
shows up in the browser and vice versa.

```
 vrotrss  │  Main stream (51)                                              unread only
 ≡ Main stream          51 │● ★ Gopher Gazette   Why your goroutines are leaking   14:26
 ★ Favourites              │●   Hacker Digest    A gentle introduction to …        13:33
 ▾ Comics               11 │●   Kernel Notes     Benchmarking the new scheduler    12:40
     Daily Strip         6 │●   Local Ledger     Notes from the kernel mailing …   10:54
     Odds and Ends       5 │●   Space Weekly     The case for boring technology    10:01
 ▾ Tech                 18 │      …
 j/k move · enter read · m read/unread · s star · A all read · u unread/all · ? help
```

## Try it without a server

```sh
make demo        # or: vrot -demo
```

## Setup

1. In FreshRSS enable the API: **Administration → Authentication → Allow API access**.
2. Set an **API password**: **Profile → API management**. This is separate from your
   login password; vrotrss uses it with your normal username.
3. Create `~/.config/vrotrss/config.toml` (`vrot -sample-config` prints a template):

```toml
url          = "https://rss.example.com"
username     = "alice"
password_cmd = "pass show freshrss/api"   # or: password = "..."

# optional, defaults shown
# mark_read_on_open = true
# unread_only       = true
# page_size         = 50
# refresh_minutes   = 5
```

`VROT_URL`, `VROT_USERNAME` and `VROT_PASSWORD` override the file. `password_cmd`
keeps the secret out of the config file (password manager, keychain, …).

## Keys

| Key | Action |
|---|---|
| `tab` | cycle sidebar → list → reader |
| `j` `k` / arrows | move / scroll · `g` `G` top / bottom |
| `enter`, `l` | open feed or folder · open article |
| `h`, `esc` | go back · fold folder |
| `space` | fold / unfold folder |
| `m` | toggle read / unread |
| `s` | toggle favourite |
| `A` | mark everything in the stream read (asks first) |
| `n` `p` | next / previous article (reader) |
| `o` | open in browser |
| `u` | toggle unread-only / all |
| `r` | refresh |
| `?` | help · `q` quit |

On terminals narrower than 70 columns it shows one pane at a time.

## How it syncs

vrotrss talks to FreshRSS through its built-in Google Reader–compatible API
(`/api/greader.php`), the same interface that mobile apps use. It never touches
FreshRSS's database directly, so it works with SQLite, MySQL and PostgreSQL
installs, in Docker or not, and survives FreshRSS upgrades. Changes are applied
immediately in the UI and sent to the server in the background; if the server
rejects one, it is rolled back and the error is shown in the footer.

Feed content is untrusted: control characters and terminal escape sequences are
stripped before anything is drawn, and only `http(s)` links can be opened.

## Install

```sh
go install github.com/projector22/vrotrss/cmd/vrot@latest   # from source (needs Go 1.26.10+; newer toolchains are fetched automatically)
```

or download an archive from the Releases page. A container image is available
via `make docker` (`docker run -it --rm -e VROT_URL=… -e VROT_USERNAME=… -e VROT_PASSWORD=… vrotrss`).

## Development

```sh
make deps      # go mod tidy + download
make check     # vet + golangci-lint + tests with -race
make cover     # coverage summary
make help      # everything else
```

Layout: `internal/greader` (API client), `internal/render` (HTML → terminal text),
`internal/tui` (Bubble Tea UI), `internal/config`, `internal/demo` (in-memory
backend for `-demo` and tests).

### Releasing

```sh
make tag-patch    # v0.1.0 -> v0.1.1   (bug fixes)
make tag-minor    # v0.1.1 -> v0.2.0   (new features)
make tag-major    # v0.2.0 -> v1.0.0   (breaking changes)
make tag VERSION=v1.4.2   # or choose the version yourself
```

Each target works out the next version from the latest `vX.Y.Z` tag (pre-releases such as
`-rc.1` are ignored; with no tags it starts from `v0.0.0`), then:

1. checks the tree is clean, you are on `main`, and `HEAD` matches `origin/main`;
2. runs `go vet`, golangci-lint and `go test -race`;
3. asks for confirmation (skipped with `YES=1` or when not run from a terminal);
4. creates an annotated tag and **pushes it**. If the push fails the local tag is removed.

Add `DRY_RUN=1` to preview the version and run the guards without running checks, tagging or
pushing. `BRANCH=` and `REMOTE=` override `main` and `origin`.

The pushed `vX.Y.Z` (or `vX.Y.Z-rc.1`) tag triggers `.github/workflows/release.yml`,
which runs the full CI suite, cross-builds archives for linux/darwin (amd64, arm64) and
windows (amd64) with `SHA256SUMS`, smoke-tests the linux binary's reported version, and
publishes a GitHub release with generated notes (tags containing `-` are marked
pre-releases). `make release VERSION=v0.1.0` does the same from your machine if you ever
need it (requires `gh`).

### CI and dependency updates

`.github/workflows/ci.yml` runs on pushes to `main`, pull requests, and (via the release
workflow) every release tag: `go vet` and `go test -race` on Linux, macOS and Windows,
golangci-lint, `go mod tidy` cleanliness, `govulncheck`, and a binary + Docker build.
Dependabot (`.github/dependabot.yml`) opens weekly grouped PRs for Go modules, GitHub
Actions and the Dockerfile base images; the CI checks above gate each one.

Commit `go.sum` (generated by `make deps`): CI, Dependabot and the Dockerfile all rely on it.

## Not yet implemented

Managing feeds (add / remove / rename), search, offline queue, per-feed sort
order. Reading, read/unread, favourites and mark-all-read are complete.

## License

MIT
