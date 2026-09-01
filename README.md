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
  text), verse media (full-size images with caption, explanation and credit,
  clips), and Research Guide references with excerpts and article links.
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
| `--no-urls` | Leave links and URLs out of the output. Link text stays in the sentence, an image becomes `[image: alt]`, and result listings drop their link line. `-o json` is unaffected. |
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

### Output without URLs

`--no-urls` strips every target from the rendered output while keeping the words
that carried it. It applies to `markdown`, `raw`, `html`, and `text`:

- a link becomes its own text, so a Bible citation still reads as a citation;
- an image becomes `[image: alt]`, or is dropped when it has no alt text;
- an image listing keeps its metadata — caption, alt text, credit, size — and
  falls back to `Image <n>` where a picture says nothing about itself, so a URL
  is never printed as a title;
- result listings print title and snippet without the link line — the result
  index still drives `jw show|open|download <n>`;
- `jw media info` lists the renditions without their file URLs.

`-o json` is deliberately untouched: it is the data model the other commands
read back, so `jw show`, `jw open` and `jw download` keep working off it. `jw
open` also still prints the link it was asked for.

```sh
jw bible read "John 3:16" --no-urls          # prose, no footnote targets
jw article 1102025912 --no-urls -o raw       # markdown without links or images
jw search --no-urls Schöpfung                # listing without link lines
```

## Commands

### Search

```sh
jw search kingdom of god                      # jw.org unified search
jw search -t videos -s newest creation        # facet + sort
jw search -n 25 -p 2 jehovah                  # pagination
jw search -e wol '(Matthew 24:14)'            # all articles citing that verse
jw search -e wol 'faith & works' --scope sen  # wol AND-search, sentence scope
jw search -e wol '(Mt 24:14)' --exclude bi,dx # without bibles and indexes
jw search -i bible study                      # interactive TUI
```

The wol engine covers every publication category by default. Which categories a
search covers is controlled by three mutually exclusive flags, also available on
`jw bible cited`:

| Flag | Meaning |
| --- | --- |
| `--all` | every category, bibles and indexes included |
| `--include w,g` | only these categories |
| `--exclude bi,dx` | every category except these |

`jw bible cited` reads every result page and prints one listing; `jw search`
pages with `-p`.

For the wol engine both commands then read each result's document and print the
passage the hit sits in — the paragraph, list item or table, whole — instead of
wol's teaser, which is cut mid-sentence. That is one request per result, run
eight at a time and cached for a week, so a repeated search costs nothing.
`--no-excerpts` skips it and keeps the teasers.

The codes are wol's own, from its "refine search" sidebar: `bi` bibles, `dx`
indexes, `w` Watchtower, `g` Awake!, `it` Insight, `bk` books, `bklt`/`brch`
brochures, `mwb` workbooks, `es` daily texts, `yb` yearbooks, `web` jw.org
pages. Which of them a language offers differs; an unknown code is rejected with
the list the language actually has.

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
jw bible read "Pr 8:8, 9"                    # single verses
jw bible read "Pr 8-9"                       # whole chapters
jw bible read "Pr 8:30-9:6"                  # across a chapter boundary
jw bible read "Joh 3:16; Ro 5:8"             # multiple references
jw bible read "Psalm 83" --bible nwt         # other editions: nwt, Rbi8, int, ...
jw bible read "Joh 3:16" --bible-all         # every edition of the language, compared
jw bible read John 3:16 --unfold 1           # verse + study notes + its references
jw bible read -l de "Matthäus 24:14"         # localized book names
jw bible notes John 3:16                     # study notes (nwtsty)
jw bible xrefs John 3:16 -r                  # cross references + full text
jw bible media John 3:16 --download          # verse images/clips w/ captions, credits
jw bible research John 3:16 -x               # research guide + excerpts
jw bible cited "Jer 31:15"                   # publications citing that verse
jw bible cited "Mt 24:14" --include w,g      # only Watchtower and Awake!
jw bible cited "Jer 31:15; Mt 2:18"          # either verse, every page
jw bible cited "Jer 31:15" --no-excerpts     # teasers only, no document reads
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

### Image metadata

Both sites serve their illustrations with EXIF/IPTC stripped, so nothing about a
picture is readable from the file itself. Everything that is known about one is
written down in the page that references it, and that is what jw-cli collects
with every image:

- **caption** and **alt text** from the figure (jw.org keeps them in the
  `data-img-att-alt`/`figcaption` markup of a responsive image, wol on the
  `<img>` tag);
- the **credit line** the sites print beside a picture (`.imgCredit`), e.g.
  `© www.BibleLandPictures.com/Alamy`;
- the **pixel size**, where the markup states it.

A study-bible verse picture keeps its explanation and its credit on the gallery
page its thumbnail links to, so `jw bible media` reads that page as well
(cached for a month) and lists the full-size rendition instead of the
thumbnail. Failing to reach it costs the extra words, not the entry.

The metadata is printed with `--no-urls` too: the flag hides where a picture is,
not what it shows. `-o json` carries it as the `image` object of a result and on
`images[]` of an article.

```sh
jw article 1102025912 --images               # caption, alt text, credit, size
jw bible media "Luke 2:7"                    # + the gallery explanation
jw show 1                                    # the same for one listed image
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
| `--images` | List its illustrations with everything the page says about them — caption, alt text, credit line, pixel size — downloadable by index. |
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

The text lands where it is cited: under the paragraph, list item or heading that
carries the citation, headed `References` and closed by a rule, rather than
gathered into an appendix at the end. Each reference becomes a heading of its
own, and at depth two and beyond what *those* passages cite nests one level
deeper under it. Every reference is expanded at most once across the whole
document — a citation repeated further down is read where it first appears —
which removes repeats and stops two passages that cite each other from looping.

An unfolded verse brings the study bible's material on it as well, so a verse
reads the way it does in the study pane:

- its **study notes** print under the verse text,
- its **marginal references** and its **research-guide passages** are references
  of that verse, so their text arrives one level deeper — `--unfold 1` gives the
  verses and their notes, `--unfold 2` also what those verses point at,
- research-guide entries naming a whole article instead of a passage have no
  passage to unfold and are listed with their link under `Research guide`.

The study bible lists a verse's publications twice — the research guide spells
each one out ("Insight, Volume 1, page 1044"), the publications index cites it by
symbol ("it-1 1044") — and `jw bible research` prints both, as the study pane
does. An expansion instead shows such a passage once, under the research guide's
citation: the two point at the same article, each cutting it where it likes, so
unfolding both would print the same text twice.

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
| `wol.jw.org` | articles, bible chapters + study pane, wol search, citations (`/bc/`, `/pc/` JSON via XHR headers), daily text, meetings, media gallery pages (image metadata) |
| `www.jw.org` | article pages reached by URL |

The client sends a browser-like User-Agent, keeps a cookie jar, and paces
wol.jw.org requests at 50 a second (`requestsPerSecond` in
`internal/httpx/client.go`, burst the same). Slow-changing data (language list,
wol library config, localized bible book names, wol search categories) is cached
under the user cache directory (`~/.cache/jw` on Linux), as are document pages
for a week — a published document does not change, and search excerpts read a
lot of them.

## Live smoke-test checklist

The full test suite runs against recorded fixtures because this project was
developed in an environment that cannot reach the live sites. The response
shapes were verified against real captures, but the following should be
smoke-tested once on a normal network:

1. `jw languages` — mediator language list and JWT-less endpoints reachable.
2. `jw search kingdom` — token fetch from `/tokens/jworg.jwt`, real TTL, and
   the 401-refresh path.
3. `jw search -e wol '(Matthew 24:14)'` and `jw bible cited "Jer 31:15"` — the
   parenthetical citation syntax, the result markup against the selectors in
   `internal/api/wol/search.go`, and the `fc[]` category filter. The category
   list is language-dependent (German has `mwbr`, English `vern`), so check a
   second language: a code the sent whitelist did not know must trigger one
   corrected retry, not silently missing results. Verified live once against
   `de` (66 hits filtered, 94 unfiltered) and `en` (74) — a large drift in
   those numbers means the filter or the selectors moved. Check the excerpts
   too: the passage under each row must be the document's, not wol's cut
   teaser. 64 of the 66 rows resolved when this was written; the rest keep the
   teaser, which is the intended fallback.
4. `jw bible read -l de "Matthäus 1:1"` — non-English `rsconf`/`lp`
   discovery and localized book-name extraction
   (`internal/api/wol/client.go`, `LocalizedBookNames`).
5. `jw bible notes/xrefs/media/research John 3:16` — study-pane selectors in
   `internal/api/wol/bible.go` (grouped in one `sel*` constant block).
6. `jw bible read --bible-all "Joh 3:16"` — the per-language bible list at
   `/{locale}/wol/bibles/{rsconf}/{lp}` and its card selectors (`Bibles` in
   `internal/api/wol/bible.go`). Which editions a language carries differs, so
   check a second language too (`-l de` has three, English eight).
7. `jw bible media "Luke 2:7"` — a verse picture: the gallery-page selectors in
   `internal/api/wol/gallery.go` (caption, credit, full-size rendition), and
   `jw article 1102025912 --images` for the figure metadata wol states inline.
8. `jw dailytext` — the `.todayItem` markup.
9. `jw dailytext --unfold 1` — an unfolded verse finds its study pane through
   the title wol answers the `/bc/` citation with ("John 3:16"), parsed as a
   reference. A live title in another shape leaves the notes out silently.
10. A video download — confirm the pub-media/mediator `checksum` fields are
    MD5 (that is what the downloader verifies).
11. wol requests from data-center IPs may hit Akamai bot protection; if you
    see 403s, try from a residential connection.

Selectors and URL patterns most likely to drift are deliberately grouped in
constants near the top of each parser file.

## Notes

- Only publicly reachable pages are supported; nothing requires a login.
- Please be considerate with download volume; this tool deliberately rate
  limits and caches.
