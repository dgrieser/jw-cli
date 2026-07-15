# jw-cli

`jw` is a command-line client for the **public** (no-login) content of
[jw.org](https://www.jw.org) and the
[Watchtower Online Library](https://wol.jw.org) (wol.jw.org):

- **Search** everything — articles, publications, videos, audio, bible hits —
  via the jw.org unified search *and* the wol library search with its special
  syntax (scripture-citation search `(Matthew 24:14)`, wildcards, `&`/`|`).
- **Read** articles and bible text as **Markdown** (default), **HTML**, plain
  **text**, or **JSON**.
- **Bible study material**: study notes, cross references (with full verse
  text), verse media (images with captions, clips), and Research Guide
  references with excerpts and article links.
- **Download** videos (quality selection), audio, publications (PDF, EPUB,
  JWPUB, MP3, ...), subtitles, and article/verse images.
- **Interactive TUI** for navigating search results and the media library.

## Build

```sh
go build -o jw ./cmd/jw
```

Requires Go 1.24+. Run the tests with `go test ./...` (they use recorded
fixtures, no network access needed).

## Global flags

| Flag | Meaning |
|---|---|
| `-l, --lang` | Content language: JW symbol (`X`), ISO code (`de`), or BCP-47 (`de-AT`). Defaults to the system locale (`LC_ALL`/`LC_MESSAGES`/`LANG`), mapped to the closest available content language. |
| `-o, --output` | Output format: `markdown` (default), `html`, `text`, or `json`. `json` emits the underlying data model of any command. |
| `-f, --file` | Write output to a file instead of stdout. |
| `-v, --verbose` | Log HTTP requests to stderr. |

## Commands

### Search

```sh
jw search kingdom of god                     # jw.org unified search
jw search -t videos -s newest creation       # facet + sort
jw search -n 25 -p 2 jehovah                 # pagination
jw search -e wol '(Matthew 24:14)'           # all articles citing that verse
jw search -e wol 'faith & works' --scope sen # wol AND-search, sentence scope
jw search -i bible study                     # interactive TUI
```

Every listing is numbered and cached, so follow-up commands take an index:

```sh
jw show 3        # render result 3 (article text, media details, ...)
jw open 3        # print its link (-b opens the browser)
jw download 3    # download it (video/audio/file)
```

### Bible

```sh
jw bible read Matthew 24:14
jw bible read "mt 24:3-14" -o text           # abbreviations, ranges
jw bible read "Joh 3:16; Ro 5:8"             # multiple references
jw bible read "Psalm 83" --bible nwt         # other editions: nwt, bi12, ...
jw bible read -l de "Matthäus 24:14"         # localized book names
jw bible notes John 3:16                     # study notes (nwtsty)
jw bible xrefs John 3:16 -r                  # cross references + full text
jw bible media John 3:16 --download          # verse images/clips w/ captions
jw bible research John 3:16 -x               # research guide + excerpts
jw bible books                               # book numbers/names
```

### Articles

```sh
jw article 1102025912                        # by MEPS document id (via wol)
jw article https://wol.jw.org/en/wol/d/r1/lp-e/1102025912
jw article <url> --refs                      # bible verses cited in the article
jw article <url> --images                    # list images (then: jw download 2)
jw article <url> --download-images -d pics/
```

### Media (JW Broadcasting library)

```sh
jw media browse                              # top-level categories
jw media browse VideoOnDemand                # drill into a category
jw media browse LatestVideos -n 25 -i        # interactive
jw media info pub-jwb_202401_1_VIDEO         # renditions of one item
```

### Publications & downloads

```sh
jw pub nwt -F PDF,EPUB                       # files of a publication
jw pub w --issue 202405                      # a Watchtower issue
jw pub nwt --booknum 40 -F MP3               # Matthew audio
jw pub w --issue 202405 --download -d out/   # download instead of listing
jw download pub-jwbcov_201505_1_VIDEO -q 720p --subtitles
jw download w --issue 202405 -F PDF
```

### Other

```sh
jw dailytext                                 # today's text (or a date)
jw meetings --date 2026-07-20                # meeting material of that week
jw languages -s german                       # language codes
jw completion bash|zsh|fish
```

## How it talks to the sites

| Backend | Used for |
|---|---|
| `b.jw-cdn.org/apis/mediator/v1` | media categories, items, language list |
| `b.jw-cdn.org/apis/pub-media/GETPUBMEDIALINKS` | publication download links |
| `b.jw-cdn.org/apis/search` + `/tokens/jworg.jwt` | unified search (anonymous JWT, auto-refreshed on 401) |
| `wol.jw.org` | articles, bible chapters + study pane, wol search, citations (`/bc/`, `/pc/` JSON via XHR headers), daily text, meetings |
| `www.jw.org` | article pages reached by URL |

The client sends a browser-like User-Agent, keeps a cookie jar, and rate
limits wol.jw.org requests (~2/s). Slow-changing data (language list, wol
library config, localized bible book names) is cached under the user cache
directory (`~/.cache/jw` on Linux).

## Live smoke-test checklist

The full test suite runs against recorded fixtures because this project was
developed in an environment that cannot reach the live sites. The response
shapes were verified against real captures, but the following should be
smoke-tested once on a normal network:

1. `jw languages` — mediator language list and JWT-less endpoints reachable.
2. `jw search kingdom` — token fetch from `/tokens/jworg.jwt`, real TTL, and
   the 401-refresh path.
3. `jw search -e wol '(Matthew 24:14)'` — the parenthetical citation syntax
   is community-documented but was not verifiable offline; also check the
   result markup matches the selectors in `internal/api/wol/search.go`.
4. `jw bible read -l de "Matthäus 1:1"` — non-English `rsconf`/`lp`
   discovery and localized book-name extraction
   (`internal/api/wol/client.go`, `LocalizedBookNames`).
5. `jw bible notes/xrefs/media/research John 3:16` — study-pane selectors in
   `internal/api/wol/bible.go` (grouped in one `sel*` constant block).
6. `jw dailytext` — the `.todayItem` markup.
7. A video download — confirm the pub-media/mediator `checksum` fields are
   MD5 (that is what the downloader verifies).
8. wol requests from data-center IPs may hit Akamai bot protection; if you
   see 403s, try from a residential connection.

Selectors and URL patterns most likely to drift are deliberately grouped in
constants near the top of each parser file.

## Notes

- Only publicly reachable pages are supported; nothing requires a login.
- Please be considerate with download volume; this tool deliberately rate
  limits and caches.
