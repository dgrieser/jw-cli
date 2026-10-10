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
- **Web server** (`jw serve`): the same features as a browsable web site and a
  JSON API under `/api/v1`.

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

Result listings are plain reports, not markdown, so glamour never touches them
— but the search APIs return titles, snippets and passages as HTML fragments,
and those are rendered: the tags and entities are resolved, and on a terminal
the parts of a row are told apart with ANSI. The index, the publication line and
the link are dim, the title is bold, the passage under a result sits behind a
quote bar, and what the search matched is highlighted inside it — wol marks its
hits in the document, and that mark is what is painted. The title carries the
result's target as an OSC 8 hyperlink, the same way a document's links are
rendered, so a listing spends no line on spelling URLs out. Long lines are
wrapped to the listing's indent. A result's kind (`article`, `video`,
`category`, ...) is in `-o json` but is not printed: it repeats itself down a
whole listing, and a duration or a file size already says what a row is.

The styling is terminal-only. Redirect, pipe, `-f|--file`, `-o raw` or
`--no-color` and a listing is byte for byte the plain report it always was, each
result on its own line and ready to grep — the quote bar included, and the link
back on a line of its own, since there is nothing to click. In a terminal
without OSC 8 support the title simply is not clickable; `jw open <n>` prints
the target of any result, and `--no-color` puts the URLs back in the listing.

The markdown is written verbatim whenever styling would get in the way:

```sh
jw article 1102025912 -o raw                 # never styled, even on a terminal
jw article 1102025912 -f out.md              # -f|--file always writes raw markdown
jw article 1102025912 | pandoc -f markdown   # a pipe is not a terminal: raw
jw article 1102025912 --no-color             # styled layout, no colors
jw bible cited "Jer 31:15" --no-color        # a listing: plain, URLs spelled out
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
- result listings print title and snippet with no target at all, neither on the
  title nor on a line of its own — the result index still drives
  `jw show|open|download <n>`;
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

A citation search matches wherever a verse is *named*, and much of the library
names verses without saying anything about them: reading schedules, scripture
indexes, school programmes, the headline over a workbook section. `jw bible
cited` leaves those out, passage by passage — a workbook that heads its section
with the reference and then asks a question about it keeps the question — and
drops a result left with nothing to say, so its count is what it prints. The
test is what remains once the reference itself is taken out of a passage:
under eight words, it was naming the verse rather than discussing it. It needs
the real passage to judge, so `--no-excerpts` shows everything unfiltered, and
`jw search` is never filtered — a search is asked for matches and should report
the matches it found.

wol has no videos, so `jw bible cited` also asks the jw.org search for the
videos quoting the verse — talks, morning worship, demonstrations — which it
finds through their transcripts. Each comes with the transcript passage quoting
the verse and is sorted in among the publications by the day it was first
published, which its line shows. A publication line names its year only, so a
video counts as newer than a publication of the same year from July on; one
whose date cannot be read, and every video of an `-s occ` listing, closes the
listing. A video that shows no transcript passage and names no verse in its
title (a song matched by its theme text) is left out.

The search's passage is a few lines of the subtitles that stop where the verse
is named, before anything is said about it. With excerpts on, each video's
subtitles are read (through the cache, like every page) and the passage is
found in them word for word. It is widened to the paragraphs it belongs to and
then to whole paragraphs before and after, until at least 150 words of context
stand on each side: the reading of the verse and what the speaker makes of it.
The reference is marked where it is said. A video found only by the verse in
its title has no passage; its excerpt is where its subtitles first say the
verse. A video without subtitles, or whose subtitles do not hold the passage,
keeps the search's passage.

`--no-videos` leaves the videos out altogether, and so does `--include`, which
names the publications to cover.

For the wol engine both commands then read each result's document and print the
passage the hit sits in — the paragraph, list item or table, whole — instead of
wol's teaser, which is cut mid-sentence. That is one request per result, run
eight at a time and cached, so a repeated search costs nothing.
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
jw bible read "Ge 1" --no-outlines           # without the book outline's headings
jw bible notes John 3:16                     # study notes (nwtsty)
jw bible xrefs John 3:16 -r                  # cross references + full text, each headed
jw bible media John 3:16 --download          # verse images/clips w/ captions, credits
jw bible research John 3:16 -x               # research guide + excerpts
jw bible cited "Jer 31:15"                   # publications citing that verse
jw bible cited "Mt 24:14" --include w,g      # only Watchtower and Awake!
jw bible cited "Jer 31:15; Mt 2:18"          # either verse, every page
jw bible cited "Jer 31:15" --no-excerpts     # teasers only, no document reads
jw bible cited "Jas 5:19" --no-videos        # publications only, no videos
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
(cached) and lists the full-size rendition instead of the
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
jw media text pub-sjjm_1_VIDEO               # a song's lyrics
jw media text pub-osg_118_AUDIO -l de        # an original song, in German
jw media text pub-mwbv_202705_1_VIDEO        # a video's transcript
jw media text pub-sjjc_1_AUDIO --timestamps  # each line's time in the recording
```

`jw media text` prints the words of a video or audio item, from the first of
these that has them:

- **The document the recording sings or reads.** pub-media lists, with the MP3
  of a publication's track, the jw.org document it belongs to and *markers*: the
  time each paragraph of that document starts in the recording. The document is
  read through the jw.org finder (`/finder?docid=…&wtlocale=…`), so it comes in
  the content language. For a song that is its lyrics — stanzas, choruses and
  bridge, the theme scripture, "See also", the printed edition and the lead
  sheet PDF when the page offers one. The songs of the songbook — for the
  meetings (`sjjm`), the vocals (`sjjc`), instrumental (`sjji`), sung by
  children (`pksjj`) — are the songbook's song of that number, whatever page
  the recording names. The original songs (`osg`), the children's songs
  (`pkon`) and the older songbook's recordings (`snv`) have their own pages.
- **The subtitles.** A video's WebVTT subtitles (linked by the mediator) are
  its transcript, joined into paragraphs at the pauses between sentences.
- **The machine-made subtitles.** For an item without subtitles of its own,
  pub-media may list an `AIVTT` file (only when the query names no file
  format). It is often listed before it is written, as an empty WebVTT file,
  and its link is signed for a few minutes only. So it is always tried when
  listed, and kept by its checksum rather than its link: an empty file is not
  downloaded again until pub-media lists a file with a new checksum (when its
  cached answer is renewed, or on a reload past the cache), and a link whose
  signature ran out is asked of pub-media anew. The transcript is labelled
  *automatic*.

Where the recording that times a document's lines is the one played (or a
rendition of the same length), each line carries its time: `--timestamps`
prints it. `--transcript` adds the subtitles' transcript under a song's lyrics.
`-o json` is the whole model: the document's blocks and lines (paragraph id,
start, end) and the transcript's cues.

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
reads the way it does in the study pane. `jw bible read` prints it in that
order, and leaves out whatever the verse does not have:

1. **Study notes**, under `Study notes`.
2. **The study bible's indexes**, each under the name the page gives it —
   `Study Guide`, then `Publications Index` — with no parent heading over the
   two. Every entry an index lists is read, whether it has a passage to unfold
   or only names an article; an entry the first index already listed is left
   out of the second.
3. **The marginal references**, under one heading naming the verse they belong
   to, each headed by the reference it points at and the verse it came from —
   which is what tells them from the references of a reference one level
   deeper. What belongs to one of them is nested under it.
4. **Quotations of …** — every publication a citation search finds quoting the
   verse, each with the passage it quotes it in, and every video quoting it,
   with the passage of its transcript: `jw bible cited` for that verse, printed
   where the verse stands. The heading names the reference it
   answers for. A quotation is where an expansion ends: what it cites is never
   unfolded in turn, and the document being unfolded is never listed as
   quoting a verse it cites itself.

Each unfolded reference is a verse in its own right, so at `--unfold 2` it
brings its own notes, indexes and margin in turn, one level deeper. The
quotations stop at the first tier: the references a source names are worth
looking up, the ones reached through them multiply the traffic without being
what was asked about. So `jw bible read` looks up the verses it prints and not
their marginal references, and a document looks up the references it writes and
not what those point at — at any `--unfold` depth.

```
## Jeremia 34:3
### Index der Publikationen
#### w80 1. 3. 24 → Befreiung! Das Ende der Christenheit überleben
### Querverweise Jeremia 34:3
#### Querverweis Jeremia 37:17 von Jeremia 34:3
#### Querverweis 2. Könige 25:6, 7 von Jeremia 34:3
### Zitate von Jeremia 34:3
```

The two directions are deduplicated against each other. The research guide cites
a passage (`it-2 528`), the search cites the document holding it (`it-2 „Rama“`);
neither line contains the other, so the research passage is resolved to the
document it sits in and a search result naming that document is left out. The
count in the heading is what is left.

A citation is looked up **as it is written**: a reference reading `Jeremia
33:1-5` is one question, not five. `jw bible read` is the exception — it asks per
verse, because the verses are what it is printing:

```sh
jw article 2014927 --unfold 1         # one lookup for each reference the article writes
jw bible read "Jer 33:1-5" --unfold 1 # five lookups, one under each verse
```

The study bible lists a verse's publications twice — the study guide spells each
one out ("Insight, Volume 1, page 1044"), the publications index cites it by
symbol ("it-1 1044") — and `jw bible research` prints both, as the study pane
does. An expansion instead shows such a passage once, under the study guide's
citation: the two point at the same article, each cutting it where it likes, so
unfolding both would print the same text twice. The publications index then
holds only what it alone lists.

The study pane lives on the chapter page, so the first verse of a chapter pays
for it and every other verse of that chapter comes free. `jw bible read` takes
`--unfold` on the same terms, on the verses it is reading:

```sh
jw bible read John 3:16 --unfold 1    # notes, research guide, margin, who quotes it
jw bible read John 3:16 --unfold 2    # and the same four for every verse those point at
```

Depth costs requests: one per reference, one per chapter page, and — for a bible
reference in the first tier — a citation search plus one read per publication it
finds, all paced at 50 a second. That last part dominates: a verse quoted 66
times costs 68 requests of its own, so a study article with 39 references runs
into the thousands. It does not grow with depth, since only the first tier is
looked up. References are followed breadth first, so the count for the next level
is known before it is spent — above 2000 it is quoted and confirmed:

```
Unfolding level 3 needs up to 4820 more requests to wol.jw.org. Continue? [y/N]
```

The count is an upper bound: verses of one chapter share its page, and a verse's
citation lookup is priced at two full pages of results before anyone knows how
many there are. Documents are cached, so a second run of the same
material spends almost nothing.

`-y, --yes` answers in advance, and is required when stdin is not a terminal,
since a script has nobody to ask. Declining stops there and the output says how
many references were left unfolded, so a partial expansion never reads as the
whole picture.

## Web server (`jw serve`)

```sh
jw serve                          # http://127.0.0.1:8080
jw serve --port 8100              # another port
jw serve -l de -v                 # German by default, log upstream requests
jw serve --addr 0.0.0.0           # expose on the network (see below)
```

`jw serve` runs the binary as a web server carrying every feature twice: a
server-rendered **web site** (no JavaScript required) and a **JSON API** under
`/api/v1`. Both are driven by the same code the commands use, so output and
behavior match the CLI.

A media item's page plays the item and shows its words under the player: a
song's lyrics, the document a recording reads, or the transcript of a video's
subtitles (behind a fold when lyrics are shown too). The line being sung or
said is highlighted as the item plays, and pressing a line or a caption's time
plays from there. The page also names the recording's publication, links the
item's page on jw.org, the printed edition and the lead sheet, and lists each
rendition's picture size and frame rate.

The server binds to `127.0.0.1` unless `--addr` says otherwise, and warns when
it is about to listen on a non-loopback address without authentication.

### Basic authentication

HTTP basic auth puts every route — web site, API and downloads — behind a
login. Either source turns it on, and both may be used together:

- **A file**: `--auth-file PATH`, or `JW_AUTH_FILE=PATH`. It may be an
  `.htpasswd` file (`user:hash` lines, as `htpasswd` writes them) or an
  `.htaccess` file whose `AuthUserFile` names one (a relative path is resolved
  against the `.htaccess` file's directory) and whose `AuthName` becomes the
  login prompt's realm. A path that does not exist is skipped, so the setting
  can stay in place while no file is mounted. Supported hashes: bcrypt
  (`htpasswd -B`, recommended), Apache MD5 (`htpasswd -m`) and SHA-1
  (`htpasswd -s`); any other entry stops the server rather than locking a user
  out silently.
- **One user from the environment**: `JW_AUTH_USER` and `JW_AUTH_PASSWORD`
  (both, or neither).

```sh
htpasswd -cB .htpasswd anne
jw serve --addr 0.0.0.0 --auth-file .htpasswd
JW_AUTH_USER=me JW_AUTH_PASSWORD=secret jw serve --addr 0.0.0.0
```

Basic auth sends the password with every request; put TLS in front (a reverse
proxy) for anything beyond a trusted network.

### Docker

```sh
make docker                                    # build jw:dev locally
docker run --rm -p 8080:8080 jw:dev            # http://127.0.0.1:8080
docker run --rm -p 8080:8080 -v jw-cache:/data jw:dev   # keep the cache
docker run --rm jw:dev languages -s german     # any CLI command works too

# with basic auth: the image looks for /config/.htaccess ($JW_AUTH_FILE) ...
docker run --rm -p 8080:8080 -v "$PWD/.htaccess:/config/.htaccess:ro" jw:dev
# ... or takes one user from the environment
docker run --rm -p 8080:8080 -e JW_AUTH_USER=me -e JW_AUTH_PASSWORD=secret jw:dev
```

The container runs as a non-root user, so a mounted auth file (and the
`AuthUserFile` it names, e.g. also under `/config`) must be world-readable.

Published images land on GHCR on every release tag (`latest`, `1.2`, `1.2.3`)
and on every push to `main` (`edge`):

```sh
docker run --rm -p 8080:8080 ghcr.io/dgrieser/jw-cli:latest
```

The image is distroless, runs as a non-root user, and defaults to
`serve --addr 0.0.0.0 --port 8080` — inside the container network the port
mapping is the boundary; without basic auth configured, publish the port to
localhost (`-p 127.0.0.1:8080:8080`) or put an authenticating reverse proxy in
front for anything internet-facing. `/data`
holds the on-disk cache; mount a volume to keep it across restarts.

What jw.org and wol.jw.org answer is kept on disk (in the user cache
directory, `/data` in the image) with no expiry, and reused — by the next
command, and by `jw serve` across restarts. For 24 hours an answer is used as
is; after that, a `HEAD` request asks upstream whether it changed (by ETag,
length or Last-Modified, whichever the site sends). If not, it is good for
another 24 hours; if so, only that page is read again, and only the results
derived from it (a parsed book grid, a library listing) are rebuilt. When
upstream cannot be reached, what is kept is used. `--cache-ttl 1h` changes the
window, `--cache-ttl 0` checks on every use.

The cache is bounded at 4 GB by default; past that, what was used least
recently is dropped. `--cache-max 500MB` or `JW_CACHE_MAX=2GB` changes the
bound, `0` turns the cache off. Downloads and signed-in search requests are
never kept.

`--lang` sets the default content language; every page and endpoint takes a
`?lang=` override (symbol, ISO code, or BCP-47, exactly like `-l`). Content
endpoints render bodies as sanitized HTML by default; `?format=markdown` and
`?format=text` select the other formats. Errors come back as
`{"error": {"code": "...", "message": "..."}}` with `400` (bad parameters),
`404` (nothing there upstream), `422` (an unfold that would need more upstream
requests than an unattended server spends), or `502` (upstream failure).

`jw bible read` prints the headings of the book's outline of contents between
the verses — for the Gospels and Acts, the headings of their overview — where
the part of the book they name begins, small and indented by depth: a nested
list in markdown, indented lines in text, and `outline` on each verse in JSON.
A passage opened in the middle of a heading is headed by it too, and only the
headings covering the verses read are shown. `--no-outlines` leaves them out
(`outlines=0` on `/api/v1/bible/read`); in the web reader the **Outline**
switch in the settings hides and shows them, remembered per browser. An edition
without an outline (Rbi8, int, ...) simply prints none.

In the web UI, every reading page — an article or publication document, the
bible reader, the daily text, and the meeting overview, midweek and weekend
parts — shows the text first and unfolds afterwards. Every verse of a reading,
and every paragraph that cites something, gets a small button in the right
margin: pick a depth (1–3) and what it references streams in right under it,
one collapsible section at a time, with the progress of the slow parts shown
in place (nothing is added when there is nothing to unfold). **Unfold all
0 1 2 3** above the text does the same for every item in turn, with a Stop
button. A page never unfolds itself by being visited: only the reader asks
for references, and what they brought is kept with the page (see below), not
in its address. Sections start collapsed and sit side by
side as chips; an opened one takes the whole row, and each reference inside it
opens on its own. Every paragraph of an unfolded passage that cites something
has a button of its own, so an expansion can be followed as deep as wanted.

Links open in place instead of leaving the page: a marginal reference (`+`), a
footnote (`*`), a bible reference or a link to an article — a table-of-contents
link like "App. C" included — opens its section under the verse or paragraph it
is in and scrolls to it — or, when it is not there yet, to where it loads,
loading just that one reference; once loaded it is opened and marked, but the
page does not move again. A verse number opens the verse in the other bibles
of the language.
Ctrl/Cmd-click still follows the link itself. A marginal reference of a verse
opens under the verse's cross references, all of them unfolded at the verse's
depth, while the verse's other sections — study notes, footnotes, indexes,
translations, quotations — appear as headings that load once opened.

A section headed by a publication — a passage of the Research Guide or an
index, a publication quoting a verse — opens and closes on its whole title; the
small ↗ at its end loads the complete article in place, as a reference (a
video's ↗ opens its player page). A section showing verses has a ↗ that opens
them in the bible reader in a new tab. Every link to something the site can
show — a reference, a footnote, a verse number, an article, a search result, a
publication in the library — can be held (a long press, or a right click) for
the rest of the choices: open it as a reference where that is how it opens,
open its page here or in a new tab, or **open it in** the section it belongs to
— the bible, the publications, media — without leaving the page: the section's
menu entry then leads there, and the section's history lists it.

Every page is kept as it was left. What was unfolded, what was open and how far
down the page was scrolled are kept in the browser (IndexedDB) and put back on
return without asking the server again — an item that found nothing, or
failed, stays that way until it is asked for again — and the menu leads back to
the last page of each section: the bible, the meetings, media, publications
(an article opened anywhere is read there) and search. Each of those sections
keeps a history of what was read in it, newest first, shown above its pages: ‹
and › step back and forth through it, and the list in between jumps to any
page of it, or drops one (a meeting is named by which meeting and which week).
The clock in the bar holds the overall history: every page read anywhere on
the site, newest first with the section it belongs to, and a step back and
forward through it. Over HTTPS (or on localhost) the browser also keeps
the pages themselves, so a page read once comes back at once, and even without
a connection: a page that reads the same whatever the day for a month, one that
shows what is current (today's text, this week's meetings, the newest videos
and issues) for the day. Behind a login, every page is asked of the server
first, so a revoked or switched login is never bypassed; the kept copy then
only stands in while the server cannot be reached at all. `--page-cache`
(`$JW_PAGE_CACHE`) sets that: `auto` (the default) does so behind this
server's own login, or a proxy's that its requests show (an `Authorization`
header, or a header naming the user such as `X-Forwarded-User` or
`Remote-User`), and serves the copy first otherwise; `first` and `fallback`
choose one way whatever the login, and `off` keeps no pages. Behind an
authenticating proxy that passes nothing on (one that only checks a cookie),
set `--page-cache fallback`.
The reload button in the bar reads the page, and everything unfolded on it,
anew from jw.org past every cache; a reference followed from a paragraph
without a button of its own is then left to be followed again.

The server counts the depth in **bible references**, the same way on every page
and in the API. A verse at depth N brings at once its study notes, its
footnotes, the passages its Research Guide and Publications Index point at,
and its marginal references — and every bible verse any of them cites, as a
verse at depth N−1. A verse at depth 0 is its text, with all of its sections as
headings that load once opened; one that turns out to have nothing says so
for a moment ("Nothing here") and then fades away. Other translations and quotations load once opened at every
depth. A passage of another publication is no step of its own: it brings the
verses it cites one reference further down than the verse or document citing
it, and nothing else it cites is followed. Depth 1 on a verse:

```
Verse
├── Study notes ───────── at once, with every bible verse they cite
├── Research Guide ────── at once, with every bible verse its passages cite
├── Publications Index ── at once, with every bible verse its passages cite
├── Marginal references ─ at once
├── Other translations ── once opened
└── Quotations ────────── once opened
        └── each bible verse reached: its text, and the same six sections
            (and footnotes, where it has any), every one loaded once opened
```

At depth 2 those verses unfold as the verse does at depth 1, and theirs wait to
be opened. In a document, a bible verse it cites is the verse unfolded at the
page's depth; a passage of another publication it cites, or an article a link
leads to, brings the verses it quotes one deeper — at depth 1 their text with
their sections to be opened. A section opened later unfolds as at depth 1.
What a link opens unfolds to the level of the verse or paragraph it is in, else
to the page's level, else to depth 1. Quotations are always the end: what they
cite unfolds only from its own button, and the documents the reader reached a
verse through — the article on the page, the passage a section shows — are left
out of who quotes it.

A verse is shown once: an expansion that reaches it again — a second study note
citing it, a marginal reference the notes already brought, a paragraph further
down an article — leaves it out, and the verse being unfolded is never repeated
under itself. **Unfold all** shares that across every item of its run, the
items streaming at once included. A reference covering verses not shown yet (a
range around one that is) is still shown.

What an expansion reads is kept in memory for the life of the server, by what
it is rather than by the link it was reached through: a verse by the verses it
holds, a chapter of the study bible with its study pane and its verses. A
citation is told by its text ("Joh 1:1") before anything is asked, so a verse
read once — through any document's link, or as part of its chapter — is not
read again at a later level, in a later stream or for the next reader, and is
neither counted nor asked about as a request.
Every loader has an abort button that stops it and keeps what already came. Every picture on a page — in an article, a verse's
notes, anything unfolded — opens full size in a new tab, from the wol.jw.org or
jw.org address it was read from.

Every page keeps a header in view: the page's title, a link up to the page
above it (the last breadcrumb, the section's start, or the page a document was
opened from), and buttons for a smaller, the default or a larger text size —
kept in this browser. On the bible reader the header names the book, chapter
and verses in view and opens a book picker: the chapters of the current book in
one row that scrolls sideways, the book grid under it, and a book picked there
shows its chapters in that row. Typing words instead of a reference into the
bible's field searches the text of the bible being read and of the other
bibles its translations section shows, and lists every hit, grouped by bible,
each passage a link into the reader.
An unfolded verse also brings its **footnotes** (only when it has any) and the
same verse in the **other translations** of the language — the New World
Translation once, as the study edition where it exists — which the web UI
loads only when that section is opened.

The **Publications** page browses the library the way wol lays it out: the
categories (Bibles, Watchtower, Awake!, Books, Meeting Workbooks, ...), then a
category's years, a year's editions and months, down to an issue or a
publication. The start page leads with the categories and rows of covers: for a
periodical its latest issues gathered across years until the row holds 24,
newest first and one row per edition (the Watchtower's public and study
editions); for books, brochures, tracts and the like the publications
themselves, newest release first (the year a publication's page records, else
the year in its first article's id). An issue or a publication shows its cover,
its table of contents (each article opens in the reader, sections of a long
publication as tabs) and its files in every format the publication media API
has, the audio tracks folded under their format; a bible opens in the bible
reader. Addresses are the same in every language: a page of the library is
named by its English path (`/pub/library/all-publications/watchtower/...`),
whichever language it is shown in, so switching the language keeps the page.
wol names its pages in each language's own words and orders them by their
titles, so the counterpart is found by what the languages share — the
publication and issue a cover shows, a symbol in the title, the kind of
publication and its year, and, among editions alike in that, their order.  Only
English paths are addresses. A page the English library does not carry — an
older book kept in German and French only — is named below its nearest English
page by its symbol (`/pub/library/all-publications/books/fm`), or by its kind
and year where it has none (`.../publications-index/dx-1945`). An address with
no counterpart in the language asked for leads back to the start page. A publication is named by its symbol
(`/pub/publication/lff`). Looking a
publication up by symbol, document id, issue, book or track
stays with `jw pub` and `GET /api/v1/pub`.

What a page remembers (see above) is kept in this browser only —
`localStorage`, IndexedDB and, over HTTPS, the page cache of the service worker
at `/sw.js`, which a new build of the server starts anew — and pages not
revisited for two months are forgotten. A citation held and opened on a page
of its own goes through `GET /open?path=/wol/bc/…`, which reads it and goes on
to the bible reader with the verses it quotes, or to the article a citation of
a publication or a footnote is part of; `GET /api/v1/open?path=…` names that
page as `{"url": …}` without going there. The bible reader also takes verses by
id: `/bible?vid=43003016&to=43003017`. Without JavaScript the switcher reloads
the page unfolded server-side, and a level that needs more requests than the
server spends unasked is offered on a confirmation page first (with
JavaScript, a prompt).

The page loads those expansions from two streaming endpoints, answered as
newline-delimited JSON events (`stage`, `progress`, `section`, `expensive`,
`error`, `done`) flushed as they happen:
`GET /unfold/verse?vid=43003016&depth=1&bible=nwtsty` (one verse, or a range
with `to=`: study notes, indexes, marginal references, quotations; `part=notes|
footnotes|indexes|marginal` — one index with `kind=guide|pubindex` — loads one
of them alone, and with `lazy=1` loads it while the others come as headings) and
`GET /unfold/refs?path=…&text=…&depth=1` (the citations of one paragraph; only
wol citation paths are followed), plus `GET /unfold/footnote?path=…`,
`GET /unfold/translations?vid=…&bible=…`, `GET /unfold/cited?vid=…&self=…`
(who quotes a passage, leaving the documents named by `self` out) and
`GET /unfold/article?url=…&depth=…` (a library document or a jw.org page; only
its path is kept and read from that site). Everything one request unfolds shares one
request budget and one set of passages already shown; the first level is
priced and, if needed, asked about before anything is spent. `done` reports
what the request spent, and **Unfold all** passes the running total of its run
as `spent=`, so the confirmation threshold covers the whole run rather than
each verse or paragraph on its own. It also names its run (`run=`, a random
token): the server keeps the verses each run has shown for an hour, so the
verse, paragraph and article streams of one run show a verse once between
them; a stream that fails gives its verses back.

| Endpoint | Parameters | CLI equivalent |
|---|---|---|
| `GET /api/v1/languages` | `q` | `jw languages -s` |
| `GET /api/v1/search` | `q`*, `engine=jworg\|wol`, `type`, `sort`, `limit` (≤50), `page`, `scope`, `all`/`include`/`exclude`, `excerpts=0` | `jw search` |
| `GET /api/v1/article` | `target`* (docid or URL), `format`, `unfold` (≤3) | `jw article` (images and scripture refs are fields of the response) |
| `GET /api/v1/bible/read` | `ref`*, `bible`, `all=true`, `unfold`, `outlines=0`, `format` | `jw bible read` |
| `GET /api/v1/bible/notes` | `ref`* | `jw bible notes` |
| `GET /api/v1/bible/xrefs` | `ref`*, `resolve=true` | `jw bible xrefs` |
| `GET /api/v1/bible/research` | `ref`*, `excerpts=true` | `jw bible research` |
| `GET /api/v1/bible/media` | `ref`* | `jw bible media` |
| `GET /api/v1/bible/cited` | `ref`*, `sort`, `scope`, category flags, `excerpts=0`, `videos=0` | `jw bible cited` |
| `GET /api/v1/bible/books` | — | `jw bible books` |
| `GET /api/v1/bible/nav` | `bible`, `book` | — (wol `/binav/`: the book grid, with `book` its chapter grid) |
| `GET /api/v1/media/categories[/{key}]` | `limit`, `offset` | `jw media browse` |
| `GET /api/v1/media/items/{lank}` | — | `jw media info` |
| `GET /api/v1/media/items/{lank}/text` | — | `jw media text` |
| `GET /api/v1/pub` | `pub` or `docid`*, `issue`, `booknum`, `track`, `fileformat`, `allLangs=true` | `jw pub` |
| `GET /api/v1/pub/library[/{path}]` | — | — (wol `/library/`: the categories, or the category, year or issue at the English `path`) |
| `GET /api/v1/pub/publication/{path}` | — | — (wol `/publication/`: a publication's table of contents) |
| `GET /api/v1/dailytext` | `date`, `format`, `unfold` | `jw dailytext` |
| `GET /api/v1/meetings[/{midweek\|weekend}]` | `date`, `format`, `unfold` | `jw meetings ...` |
| `GET /download/media/{lank}` | `quality`, `subtitles=true` | `jw download LANK -q` |
| `GET /download/pub` | pub parameters | `jw pub --download` |

```sh
curl 'http://127.0.0.1:8080/api/v1/search?q=kingdom&limit=5'
curl 'http://127.0.0.1:8080/api/v1/bible/read?ref=John+3:16&lang=de&format=markdown'
curl -L -o video.mp4 'http://127.0.0.1:8080/download/media/pub-jwbcov_201505_1_VIDEO?quality=720p'
```

A few CLI affordances have no server counterpart by design: the TUI and
`open --browser` are terminal affairs; the index-based `show`/`open`/
`download <n>` follow-ups are replaced by explicit parameters (every listing
item carries its `wolLink`/`jwLink`/`lank`/`fileUrl`); and nothing is ever
written to the server's filesystem — the `/download/...` endpoints do the
quality/format selection, then answer `302 Found` with the file's public CDN
URL instead of proxying the bytes. One shared HTTP client paces all upstream
traffic, so concurrent requests queue against the same polite rate limit the
CLI keeps.

The server runs the same expansions and listings the CLI does, defaults
included: excerpts are read for wol searches and citation listings
(`excerpts=0` turns them off). Expansions are the exception: `unfold` counts in
bible references as the web pages do (above), with who quotes a verse and its
other translations left as headings whose body loads from the `/unfold/…`
address they carry. That costs upstream requests, and the CLI asks before spending a lot
of them, so the server asks too: past 2000 requests for one level the API
answers `422 too_expensive` naming the count, and the web pages offer the count
with a link that repeats the request. `force=1` is the answer given in advance
— what `-y` is on the command line — and `unfold` stays capped at depth 3. A
study article with dozens of references costs thousands of requests, so forcing
one takes minutes: the command line is the better place for it.

A bible reading folds what it brings. Each verse's study notes, indexes,
marginal references and quotations become a disclosure of its own, closed until
it is opened, so the verses stay readable however much hangs off them.

## How it talks to the sites

| Backend | Used for |
|---|---|
| `b.jw-cdn.org/apis/mediator/v1` | media categories, items, language list |
| `b.jw-cdn.org/apis/pub-media/GETPUBMEDIALINKS` | publication download links |
| `b.jw-cdn.org/apis/search` + `/tokens/jworg.jwt` | unified search (anonymous JWT, auto-refreshed on 401) |
| `wol.jw.org` | articles, bible chapters + study pane, wol search, citations and marginal references (`/bc/`, `/pc/`, `/marginalreference/` JSON, requested without the locale segment), daily text, meetings, media gallery pages (image metadata), the publication tree (`/library/`, `/publication/`) |
| `www.jw.org` | article pages reached by URL |

Those three endpoints answer with the passage itself only when asked **without**
the locale segment — `/wol/bc/…` rather than `/de/wol/bc/…`, which redirects to
the whole page — and they answer in kind, with locale-less links. The locale the
caller asked with is put back into what comes out, or every link in an unfolded
passage would lead nowhere.

The client sends a browser-like User-Agent, keeps a cookie jar, and paces
wol.jw.org requests at 50 a second (`requestsPerSecond` in
`internal/httpx/client.go`, burst the same). Response bodies are kept under the
user cache directory (`~/.cache/jw` on Linux, `http/`) with their validators,
and revalidated by `HEAD` once older than the freshness window
(`internal/httpx/revalidate.go`); a server that cannot answer a `HEAD` usefully
— the CDN's mediator API answers it with 404, jw.org renders some pages per
request — is remembered and asked with a `GET` instead, and an equal body is
still no change. Results derived from bodies (language list, localized bible
book names, book grids, library pages) are memos (`memo/`, `httpx.Memo`) that
record the bodies they were built from and are rebuilt only when one of those
changed. Concurrent reads of one page share one request, and the least recently
used entries are evicted beyond `--cache-max`.

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
