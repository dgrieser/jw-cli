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
make build          # ./jw, reports dev+<commit>[-dirty]; VERSION=v1.2.3 to override
make install        # into $GOBIN
```

Requires Go 1.25+. Tests use recorded fixtures, so no network access is needed.

Development targets (`make help` lists them all):

```sh
make test           # go test ./...
make test-race      # with the race detector
make cover          # coverage.out + coverage.html
make fmt            # gofmt + goimports
make lint           # golangci-lint, includes the modernize suite
make modernize-fix  # apply modern-Go rewrites in place
make check          # fmt-check + vet + lint + test-race (what CI runs)
make snapshot       # local goreleaser build of all release artifacts
```

golangci-lint and goreleaser are used from `$PATH` when present, otherwise
fetched at pinned versions via `go run`; `make tools` installs them.

## Global flags

| Flag | Meaning |
|---|---|
| `-l, --lang` | Content language: JW symbol (`X`), ISO code (`de`), or BCP-47 (`de-AT`). Defaults to the system locale (`LC_ALL`/`LC_MESSAGES`/`LANG`), mapped to the closest available content language. Also selects the language of jw-cli's own labels and dates — see below. |
| `-o, --output` | Output format: `markdown` (default), `raw`, `html`, `text`, or `json`. `json` emits the underlying data model of any command. |
| `-f, --file` | Write output to a file instead of stdout. |
| `--no-color` | Render markdown without ANSI colors (also honors `NO_COLOR`). |
| `-v, --verbose` | Log HTTP requests to stderr. |
| `--version` | Print the version, commit, build date, Go version, and platform. Released binaries report their tag (`v1.2.3`); local builds report `dev+<commit>` plus `-dirty` for an uncommitted tree. |

### Output language

The headings, labels, hints and dates jw-cli prints follow `-l|--lang`, so an
article is not framed in English. German and English are translated; every other
language keeps its own content inside an English frame.

```sh
jw dailytext -l de     # "# Tagestext, Montag, 3. August 2026"
jw dailytext -l E      # "# Daily text, Monday, August 3, 2026"
jw dailytext -l F      # French content, English frame
```

Translations live in `internal/i18n`: one `Messages` struct, one catalog file per
language. Adding a message is a compile error until every language carries it,
and a test rejects an empty translation or one whose format verbs drifted from
English. `GLAMOUR_STYLE` and the command help (`--help`) stay English.

### Markdown on the terminal

With the default `-o markdown`, article, Bible, and media output is styled for
the terminal — headings, lists, emphasis, and quotes get colors, and paragraphs
are wrapped to the terminal width.

Links are rendered as OSC 8 hyperlinks on their own text, so a Bible text reads
as prose instead of being broken up by URLs: the verse numbers and footnote
markers in `jw bible read` are clickable, the target stays out of the way. A URL
that is its own text (`jw media info` file lists) still shows in full. In a
terminal without hyperlink support the text simply is not clickable — use
`-o raw` there if you need the targets.

On a terminal, a command's output is framed by one blank line above and below so
it stands off from the shell prompt. The frame is opened once per run, not per
line, and a pipe, a redirect, `-f|--file` and `-o raw` get the bytes unchanged.

Result listings are plain reports, not markdown, so they are never restyled —
but the search APIs return titles and snippets as HTML fragments, and those are
rendered too: the tags and entities are resolved, and on a terminal the matched
words keep the API's highlight as bold. Long titles and snippets are wrapped to
the listing's indent; links are left whole so they stay clickable. Redirect or
pipe a listing and each result stays on its own line, ready to grep.

The markdown is written verbatim whenever styling would get in the way:

```sh
jw article 1102025912 -o raw                 # never styled, even on a terminal
jw article 1102025912 -f out.md              # -f|--file always writes raw markdown
jw article 1102025912 | pandoc -f markdown   # a pipe is not a terminal: raw
jw article 1102025912 --no-color             # styled layout, no colors
```

The color scheme follows the terminal background. Override it with
`GLAMOUR_STYLE` (`dark`, `light`, `ascii`, `notty`, `dracula`, `tokyo-night`,
`pink`), which also skips the background-color query:

```sh
GLAMOUR_STYLE=light jw dailytext
```

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
jw bible read John 3:16 --unfold 1           # verse + study notes + its references
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
jw meetings                                  # this week's meeting material
jw meetings midweek                          # the Life and Ministry workbook part
jw meetings weekend --date 2026-07-20        # that week's Watchtower study article
jw languages -s german                       # language codes
jw completion bash|zsh|fish
```

`jw meetings` lists what each meeting covers and which publications it uses;
`midweek` (`mid`, `mw`) and `weekend` (`we`, `wt`) read the material itself. The
two meetings are recognized by the publication symbol wol tags them with
(`pub-mwb`, `pub-w`), not by the section headings, so it works in any language.

Every command that ends in a document — `article`, `dailytext`, `meetings` and
the two meeting parts — takes the same flags for what to do with it:

| Flag | Meaning |
|---|---|
| `--refs` | List the bible verses the document cites, each linked to the verse. |
| `--images` | List its illustrations with captions, downloadable by index. |
| `--download-images` | Download all of them, with `-d, --dir` for the target. |
| `--unfold N` | Print the text behind every citation, following references `N` levels deep. Verses also bring their study material. |

```sh
jw meetings midweek --refs           # the verses that week's part cites
jw meetings weekend --images         # the study article's illustrations
jw dailytext --refs                  # today's cited verses
```

### Unfolding citations

wol writes a bible verse or a passage of another publication as a link. `--unfold`
follows those links and brings the text itself into the output, so a document can
be read without leaving it:

```sh
jw dailytext --unfold 1               # today's text plus every verse it cites
jw meetings midweek --unfold 1        # the workbook part with its passages
jw dailytext --unfold 2 --yes         # and the cross references inside those verses
```

Each reference becomes a heading under `References`, and at depth two and beyond
what *those* passages cite nests one level deeper. Every reference is expanded at
most once across the whole document, which removes repeats and stops two passages
that cite each other from looping.

An unfolded verse brings the study bible's material on it as well, so a verse
reads the way it does in the study pane:

- its **study notes** print under the verse text,
- its **marginal references** and its **research-guide passages** are references
  of that verse, so their text arrives one level deeper — `--unfold 1` gives the
  verses and their notes, `--unfold 2` also what those verses point at,
- research-guide entries naming a whole article instead of a passage have no
  passage to unfold and are listed with their link under `Research guide`.

The study pane lives on the chapter page, so the first verse of a chapter pays
for it and every other verse of that chapter comes free. `jw bible read` takes
`--unfold` on the same terms, on the verses it is reading:

```sh
jw bible read John 3:16 --unfold 1    # the verse, its study notes, its research entries
jw bible read John 3:16 --unfold 2    # and the text behind each of those references
```

Depth costs requests: one per reference plus one per chapter page, paced at 20 a
second. References are followed breadth first, so the count for the next level is
known before it is spent — above 2000 it is quoted and confirmed:

```
Unfolding level 3 needs up to 4820 more requests to wol.jw.org. Continue? [y/N]
```

The count is an upper bound because verses of one chapter share its page.

`-y, --yes` answers in advance, and is required when stdin is not a terminal,
since a script has nobody to ask. Declining stops there and the output says how
many references were left unfolded, so a partial expansion never reads as the
whole picture.

## How it talks to the sites

| Backend | Used for |
|---|---|
| `b.jw-cdn.org/apis/mediator/v1` | media categories, items, language list |
| `b.jw-cdn.org/apis/pub-media/GETPUBMEDIALINKS` | publication download links |
| `b.jw-cdn.org/apis/search` + `/tokens/jworg.jwt` | unified search (anonymous JWT, auto-refreshed on 401) |
| `wol.jw.org` | articles, bible chapters + study pane, wol search, citations (`/bc/`, `/pc/` JSON via XHR headers), daily text, meetings |
| `www.jw.org` | article pages reached by URL |

The client sends a browser-like User-Agent, keeps a cookie jar, and paces
wol.jw.org requests at 20 a second (`wolRequestsPerSecond` in
`internal/httpx/client.go`, burst the same). Slow-changing data (language list,
wol library config, localized bible book names) is cached under the user cache
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
7. `jw dailytext --unfold 1` — an unfolded verse finds its study pane through
   the title wol answers the `/bc/` citation with ("John 3:16"), parsed as a
   reference. A live title in another shape leaves the notes out silently.
8. A video download — confirm the pub-media/mediator `checksum` fields are
   MD5 (that is what the downloader verifies).
9. wol requests from data-center IPs may hit Akamai bot protection; if you
   see 403s, try from a residential connection.

Selectors and URL patterns most likely to drift are deliberately grouped in
constants near the top of each parser file.

## Notes

- Only publicly reachable pages are supported; nothing requires a login.
- Please be considerate with download volume; this tool deliberately rate
  limits and caches.
