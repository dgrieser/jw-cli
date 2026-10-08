// jw serve — what the pages need from a script: a menu button on small
// screens, a visible "loading" state while the server talks to jw.org, and the
// unfolding of a document after it is on screen: every verse or paragraph that
// cites something gets a button that loads what it references, section by
// section, as the server finds it. The bible, meeting, media and publication
// pages remember what the reader had in front of them — which page, what was
// unfolded, what was open, how far down — and bring it back on their return.
// Every page works without it; this only makes reading, waiting and
// navigating nicer.
(function () {
  "use strict";

  var root = document.documentElement;

  // --- menu ---------------------------------------------------------------

  var header = document.querySelector(".page-bar");
  var toggle = document.querySelector(".menu-toggle");

  function setMenu(open) {
    if (!header || !toggle) return;
    header.classList.toggle("open", open);
    toggle.setAttribute("aria-expanded", open ? "true" : "false");
  }

  if (toggle) {
    toggle.addEventListener("click", function () {
      setMenu(!header.classList.contains("open"));
    });
    document.addEventListener("keydown", function (e) {
      if (e.key === "Escape" && header.classList.contains("open")) {
        setMenu(false);
        toggle.focus();
      }
    });
    document.addEventListener("click", function (e) {
      if (header.classList.contains("open") && !header.contains(e.target)) setMenu(false);
    });
  }

  // --- text size -----------------------------------------------------------

  // the size is a percentage of the browser's own, kept in this browser; the
  // head of the page applies it before anything is drawn
  var FONT_STEPS = [80, 90, 100, 112, 125, 140, 160];
  function fontSize() {
    var f = parseFloat(root.style.fontSize);
    return f > 0 ? f : 100;
  }
  document.querySelectorAll(".pb-font [data-font]").forEach(function (b) {
    b.addEventListener("click", function () {
      var dir = parseInt(b.getAttribute("data-font"), 10) || 0;
      var cur = fontSize();
      var next = 100;
      if (dir > 0) {
        next = FONT_STEPS[FONT_STEPS.length - 1];
        for (var i = 0; i < FONT_STEPS.length; i++) {
          if (FONT_STEPS[i] > cur + 0.5) { next = FONT_STEPS[i]; break; }
        }
      } else if (dir < 0) {
        next = FONT_STEPS[0];
        for (var j = FONT_STEPS.length - 1; j >= 0; j--) {
          if (FONT_STEPS[j] < cur - 0.5) { next = FONT_STEPS[j]; break; }
        }
      }
      root.style.fontSize = next === 100 ? "" : next + "%";
      try {
        if (next === 100) window.localStorage.removeItem("jw:font");
        else window.localStorage.setItem("jw:font", String(next));
      } catch (err) {
        // not kept: the size holds for this page only
      }
    });
  });

  // the size controls are a menu behind one button: open until a click
  // elsewhere or Escape, so a size can be stepped through
  (function () {
    var btn = document.querySelector(".pb-font-toggle");
    var menu = document.getElementById("font-menu");
    if (!btn || !menu) return;
    function setOpen(open) {
      menu.hidden = !open;
      btn.setAttribute("aria-expanded", open ? "true" : "false");
    }
    btn.addEventListener("click", function (e) {
      e.stopPropagation();
      setOpen(menu.hidden);
    });
    document.addEventListener("click", function (e) {
      if (!menu.hidden && !menu.contains(e.target)) setOpen(false);
    });
    document.addEventListener("keydown", function (e) {
      if (e.key === "Escape" && !menu.hidden) {
        setOpen(false);
        btn.focus();
      }
    });
  })();

  // --- reloading past the cache ----------------------------------------------

  // the bar's reload asks the server for the page once more with ?refresh=1:
  // what it read from jw.org is read anew. The address drops the parameter at
  // once, so the next reload is an ordinary one; what was unfolded is asked
  // for anew too, rather than put back as it was kept.
  var REFRESH = false;
  (function () {
    var u = new URL(location.href);
    if (u.searchParams.get("refresh") === "1") {
      REFRESH = true;
      u.searchParams.delete("refresh");
      history.replaceState(history.state, "", u.pathname + u.search + u.hash);
    }
    var btn = document.querySelector(".pb-refresh");
    if (!btn) return;
    btn.addEventListener("click", function () {
      var to = new URL(location.href);
      to.searchParams.set("refresh", "1");
      location.replace(to.pathname + to.search + to.hash);
    });
  })();

  // --- the page's header ------------------------------------------------------

  // the bar that stays in view names what the page shows and leads up to the
  // page above it: the last breadcrumb, the section a page belongs to, or —
  // for a document opened from another page of the site — where it came from
  var pageBar = document.querySelector(".page-bar");
  (function () {
    if (!pageBar) return;
    var title = pageBar.querySelector(".pb-title");
    if (title && !title.textContent.trim()) {
      var h = document.querySelector("main .document h1, main h1, main h2");
      title.textContent = (h ? h.textContent : document.title.replace(/\s*·\s*JW$/, "")).replace(/\s+/g, " ").trim();
    }
    var up = pageBar.querySelector(".pb-up");
    if (!up) return;
    var href = "";
    var crumbs = document.querySelectorAll("main .crumbs a[href]");
    if (crumbs.length) href = crumbs[crumbs.length - 1].getAttribute("href");
    var nav = document.querySelector(".site-nav a.active[href]");
    if (!href && nav) {
      var u = new URL(nav.href, location.href);
      if (u.pathname !== location.pathname) href = nav.getAttribute("href");
    }
    // a page of its own, outside the menu's sections: back where it was opened
    if (!href && !nav && document.referrer) {
      try {
        var r = new URL(document.referrer);
        if (r.origin === location.origin && (r.pathname !== location.pathname || r.search !== location.search)) {
          href = r.pathname + r.search;
        }
      } catch (err) {
        // no way back to name
      }
    }
    if (href) {
      up.setAttribute("href", href);
      up.hidden = false;
    }
  })();

  // --- the bible's header: the verses in view, and a way to another book -----

  (function () {
    var btn = document.querySelector(".pb-bible");
    if (!btn) return;
    var title = btn.querySelector(".pb-title");
    var names = {};
    try {
      names = JSON.parse((document.getElementById("bible-books") || {}).textContent || "{}") || {};
    } catch (err) {
      names = {};
    }
    var edition = btn.getAttribute("data-edition") || "nwtsty";
    var lang = new URLSearchParams(location.search).get("lang") || "";
    var currentBook = parseInt(btn.getAttribute("data-book"), 10) || 0;
    var currentChapter = 0;

    // the verses in view, as a reference: the book and chapter of the first,
    // the verses of that chapter on screen
    var verses = Array.prototype.slice.call(document.querySelectorAll(".document .item[data-vid]"));
    if (verses.length && window.IntersectionObserver) {
      var visible = new Set();
      var update = function () {
        var ids = [];
        visible.forEach(function (v) { ids.push(v); });
        if (!ids.length) return;
        ids.sort(function (a, b) { return a - b; });
        var first = ids[0];
        var book = Math.floor(first / 1e6);
        var chapter = Math.floor(first / 1e3) % 1e3;
        var last = first;
        ids.forEach(function (id) {
          if (Math.floor(id / 1e3) === Math.floor(first / 1e3)) last = Math.max(last, id);
        });
        currentBook = book;
        currentChapter = chapter;
        var from = first % 1e3;
        var to = last % 1e3;
        title.textContent = (names[book] || "") + " " + chapter + ":" + from + (to > from ? "–" + to : "");
      };
      var io = new IntersectionObserver(function (entries) {
        entries.forEach(function (e) {
          var id = parseInt(e.target.getAttribute("data-vid"), 10);
          if (e.isIntersecting) visible.add(id);
          else visible.delete(id);
        });
        update();
      }, { rootMargin: "-" + (pageBar ? pageBar.offsetHeight : 0) + "px 0px 0px 0px" });
      // only the verse's own text counts, not what was unfolded under it
      verses.forEach(function (v) {
        var t = v.querySelector(":scope > .item-text");
        if (t) {
          t.setAttribute("data-vid", v.getAttribute("data-vid"));
          io.observe(t);
        }
      });
    } else if (verses.length) {
      var id0 = parseInt(verses[0].getAttribute("data-vid"), 10);
      currentBook = Math.floor(id0 / 1e6);
      currentChapter = Math.floor(id0 / 1e3) % 1e3;
    }
    if (!currentBook && verses.length) {
      var id1 = parseInt(verses[0].getAttribute("data-vid"), 10);
      currentBook = Math.floor(id1 / 1e6);
      currentChapter = Math.floor(id1 / 1e3) % 1e3;
    }

    var picker = biblePicker({
      edition: edition,
      lang: lang,
      names: names,
      current: function () { return { book: currentBook, chapter: currentChapter }; }
    });
    btn.addEventListener("click", picker.open);
  })();

  // --- the bible's picker: a book's chapters as one row, the books under it --
  //
  // Opened at the book being read, its chapters run in a row above the books.
  // A book picked from the grid shows its chapters the way the bible page does
  // — a heading and a grid — with the row gone, and a way back to the books.
  // Away from the bible there is no book being read: the books, then their
  // chapters. A chapter is a link: it opens the bible there.

  function biblePicker(opts) {
    var edition = opts.edition || "nwtsty";
    var lang = opts.lang || "";
    var names = opts.names || {};
    var dialog = null;
    var row = null;
    var rowTitle = null;
    var grid = null;
    var view = null;
    var navCache = {};
    var cur = { book: 0, chapter: 0 };

    function api(book) {
      var q = new URLSearchParams({ bible: edition });
      if (book) q.set("book", String(book));
      if (lang) q.set("lang", lang);
      var key = q.toString();
      if (!navCache[key]) {
        navCache[key] = fetch("/api/v1/bible/nav?" + key, { credentials: "same-origin" }).then(function (res) {
          if (!res.ok) throw new Error(res.status + " " + res.statusText);
          return res.json();
        });
        navCache[key].catch(function () { delete navCache[key]; });
      }
      return navCache[key];
    }

    function chapterHref(book, chapter) {
      var q = new URLSearchParams({ bible: edition, book: String(book), chapter: String(chapter) });
      if (lang) q.set("lang", lang);
      return "/bible?" + q.toString();
    }

    function markBook(book) {
      grid.querySelectorAll(".book a").forEach(function (a) {
        a.classList.toggle("active", parseInt(a.getAttribute("data-book"), 10) === book);
      });
    }

    // the books, with the chapters of the book being read in the row above
    function showBooks() {
      view.hidden = true;
      grid.hidden = false;
      row.textContent = "";
      var book = cur.book;
      markBook(book);
      if (!book) {
        row.hidden = true;
        rowTitle.textContent = T.pickBook || "";
        return;
      }
      row.hidden = false;
      // the book's regular name, with the chapter being read — never the long
      // title the library heads the book with
      var label = function () {
        var n = names[book] || "";
        return cur.chapter ? n + " " + cur.chapter : n;
      };
      rowTitle.textContent = label();
      api(book).then(function (nav) {
        if (row.hidden || cur.book !== book) return;
        rowTitle.textContent = label();
        row.textContent = "";
        var chosen = null;
        (nav.chapters || []).forEach(function (c) {
          var a = document.createElement("a");
          a.href = chapterHref(book, c);
          a.textContent = String(c);
          if (c === cur.chapter) {
            a.className = "active";
            a.setAttribute("aria-current", "true");
            chosen = a;
          }
          row.appendChild(a);
        });
        var target = chosen || row.firstElementChild;
        if (target) row.scrollLeft = Math.max(0, target.offsetLeft - row.clientWidth / 2 + target.offsetWidth / 2);
      }, function (err) {
        row.textContent = String(err && err.message || err);
      });
      grid.scrollTop = 0;
    }

    // one book's chapters as the bible page lays them out: the row is gone
    function showBook(book) {
      row.hidden = true;
      grid.hidden = true;
      view.hidden = false;
      rowTitle.textContent = names[book] || "";
      var list = view.querySelector(".chapter-grid");
      var heading = view.querySelector("h2");
      heading.textContent = names[book] || "";
      list.textContent = T.loading || "…";
      view.scrollTop = 0;
      view.setAttribute("data-book", String(book));
      api(book).then(function (nav) {
        if (view.getAttribute("data-book") !== String(book)) return;
        heading.textContent = nav.title || names[book] || "";
        list.textContent = "";
        (nav.chapters || []).forEach(function (c) {
          var li = document.createElement("li");
          var a = document.createElement("a");
          a.href = chapterHref(book, c);
          a.textContent = String(c);
          if (book === cur.book && c === cur.chapter) {
            a.className = "active";
            a.setAttribute("aria-current", "true");
          }
          li.appendChild(a);
          list.appendChild(li);
        });
        var first = list.querySelector("a");
        if (first) first.focus({ preventScroll: true });
      }, function (err) {
        list.textContent = String(err && err.message || err);
      });
    }

    function build() {
      dialog = document.createElement("dialog");
      dialog.className = "bible-picker";
      dialog.setAttribute("aria-label", T.pickBook || "");
      dialog.innerHTML = '<div class="bp-head"><strong class="bp-book"></strong>' +
        '<button type="button" class="bp-close" aria-label="×">×</button></div>' +
        '<div class="bp-chapters" role="list"></div><div class="bp-books"></div>' +
        '<div class="bp-book-view bible-nav chapters-nav" hidden>' +
        '<p class="bible-nav-back"><a href="#" class="bp-back"></a></p><h2></h2>' +
        '<h3 class="bible-nav-heading"></h3><ul class="chapter-grid"></ul></div>';
      row = dialog.querySelector(".bp-chapters");
      rowTitle = dialog.querySelector(".bp-book");
      grid = dialog.querySelector(".bp-books");
      view = dialog.querySelector(".bp-book-view");
      var back = view.querySelector(".bp-back");
      back.textContent = "‹ " + (T.allBooks || "");
      back.addEventListener("click", function (e) {
        e.preventDefault();
        showBooks();
      });
      view.querySelector(".bible-nav-heading").textContent = T.chapters || "";
      dialog.querySelector(".bp-close").addEventListener("click", function () { dialog.close(); });
      // a click on the backdrop closes it
      dialog.addEventListener("click", function (e) {
        if (e.target === dialog) dialog.close();
      });
      document.body.appendChild(dialog);
      grid.textContent = T.loading || "…";
      api(0).then(function (nav) {
        grid.textContent = "";
        (nav.sections || []).forEach(function (sec) {
          if (sec.heading) {
            var h = document.createElement("h3");
            h.className = "bible-nav-heading";
            h.textContent = sec.heading;
            grid.appendChild(h);
          }
          var ul = document.createElement("ul");
          ul.className = "book-grid " + (sec.key || "");
          (sec.books || []).forEach(function (b) {
            if (!names[b.number]) names[b.number] = b.name;
            var li = document.createElement("li");
            li.className = "book " + (b.group || "");
            var a = document.createElement("a");
            a.href = "/bible?" + new URLSearchParams(Object.assign({ bible: edition, book: String(b.number) }, lang ? { lang: lang } : {})).toString();
            a.setAttribute("data-book", String(b.number));
            if (b.abbreviation) a.title = b.name;
            var n = document.createElement("span");
            n.className = "name";
            n.textContent = b.name;
            a.appendChild(n);
            if (b.abbreviation) {
              var ab = document.createElement("span");
              ab.className = "abbr";
              ab.textContent = b.abbreviation;
              a.appendChild(ab);
            }
            a.addEventListener("click", function (e) {
              if (e.metaKey || e.ctrlKey || e.shiftKey || e.altKey || e.button !== 0) return;
              e.preventDefault();
              e.stopPropagation();
              showBook(b.number);
            });
            li.appendChild(a);
            ul.appendChild(li);
          });
          grid.appendChild(ul);
        });
        // the names are known now: the row's title can say the book
        if (!grid.hidden) showBooks();
      }, function (err) {
        grid.textContent = String(err && err.message || err);
      });
    }

    return {
      open: function () {
        cur = opts.current ? opts.current() : { book: 0, chapter: 0 };
        if (!dialog) build();
        showBooks();
        if (dialog.showModal) dialog.showModal();
        else dialog.setAttribute("open", "");
      }
    };
  }

  // --- the bar's bible: away from the bible, a way to a chapter of it ---------

  (function () {
    var btn = document.querySelector(".pb-jump");
    if (!btn) return;
    var lang = new URLSearchParams(location.search).get("lang") || "";
    // the edition last read in this language, else the study bible
    var edition = "nwtsty";
    try {
      var last = window.localStorage.getItem("jw:last:bible:" + lang);
      if (last) edition = new URL(last, location.href).searchParams.get("bible") || edition;
    } catch (err) {
      // nothing remembered: the study bible
    }
    btn.addEventListener("click", biblePicker({ edition: edition, lang: lang }).open);
  })();

  // --- the content language: a dialog behind the translate glyph in the bar -

  (function () {
    var btn = document.querySelector(".lang-toggle");
    if (!btn) return;
    var current = btn.getAttribute("data-current") || "";
    var dialog, select;

    function label(l) {
      var v = l.vernacular || l.name || l.symbol;
      return l.name && l.name !== v ? v + " (" + l.name + ")" : v;
    }

    // the browser's own language, as the first content language that matches
    // one of its preferred tags exactly, or else by the tag's language alone
    function detect(langs) {
      var tags = (navigator.languages && navigator.languages.length ? navigator.languages : [navigator.language || ""])
        .map(function (t) { return String(t).toLowerCase(); }).filter(Boolean);
      var spoken = langs.filter(function (l) { return !l.isSignLanguage && l.locale; });
      for (var i = 0; i < tags.length; i++) {
        var tag = tags[i], base = tag.split("-")[0];
        var hit = spoken.find(function (l) { return l.locale.toLowerCase() === tag; }) ||
          spoken.find(function (l) { return l.locale.toLowerCase() === base; });
        if (hit) return hit;
      }
      return null;
    }

    // the languages picked here, newest first, kept in this browser
    var RECENT_KEY = "jw:recent-langs";
    function recent() {
      try {
        var list = JSON.parse(localStorage.getItem(RECENT_KEY) || "[]");
        return Array.isArray(list) ? list.filter(function (s) { return typeof s === "string"; }).slice(0, 3) : [];
      } catch (err) {
        return [];
      }
    }
    function remember(sym) {
      try {
        var list = [sym].concat(recent().filter(function (s) { return s !== sym; })).slice(0, 3);
        localStorage.setItem(RECENT_KEY, JSON.stringify(list));
      } catch (err) {
        // not kept: the quick list just lacks it
      }
    }

    function option(l) {
      var o = document.createElement("option");
      o.value = l.symbol;
      o.textContent = label(l);
      return o;
    }

    function fill(langs) {
      select.textContent = "";
      var quick = document.createElement("optgroup");
      quick.label = T.langQuick || "";
      var bySymbol = {};
      langs.forEach(function (l) { bySymbol[l.symbol] = l; });
      // English, the browser's language, then the last ones picked here
      var picks = [bySymbol.E, detect(langs)].concat(recent().map(function (sym) { return bySymbol[sym]; }));
      var seen = {};
      picks.forEach(function (l) {
        if (!l || seen[l.symbol]) return;
        seen[l.symbol] = true;
        quick.appendChild(option(l));
      });
      var all = document.createElement("optgroup");
      all.label = T.langAll || "";
      langs.forEach(function (l) { all.appendChild(option(l)); });
      select.appendChild(quick);
      select.appendChild(all);
      // the language in use shows as chosen: in the quick group when it is
      // there, so the long list does not open scrolled far down
      var chosen = select.querySelector('option[value="' + CSS.escape(current) + '"]');
      if (chosen) chosen.selected = true;
      else select.selectedIndex = -1;
      select.disabled = false;
    }

    function buildDialog() {
      dialog = document.createElement("dialog");
      dialog.className = "lang-picker";
      dialog.setAttribute("aria-label", T.language || "");
      dialog.innerHTML = '<div class="bp-head"><strong class="bp-book"></strong>' +
        '<button type="button" class="bp-close" aria-label="×">×</button></div>' +
        '<div class="lp-body"><select class="lp-select" disabled></select></div>';
      dialog.querySelector(".bp-book").textContent = T.language || "";
      select = dialog.querySelector(".lp-select");
      select.setAttribute("aria-label", T.language || "");
      var loading = document.createElement("option");
      loading.textContent = T.loading || "…";
      select.appendChild(loading);
      dialog.querySelector(".bp-close").addEventListener("click", function () { dialog.close(); });
      dialog.addEventListener("click", function (e) {
        if (e.target === dialog) dialog.close();
      });
      select.addEventListener("change", function () {
        if (!select.value || select.value === current) return;
        remember(select.value);
        var u = new URL(location.href);
        u.searchParams.set("lang", select.value);
        location.assign(u.toString());
      });
      document.body.appendChild(dialog);
      fetch("/api/v1/languages", { headers: { Accept: "application/json" } })
        .then(function (r) {
          if (!r.ok) throw new Error(r.status + " " + r.statusText);
          return r.json();
        })
        .then(fill, function (err) {
          loading.textContent = String(err && err.message || err);
        });
    }

    btn.addEventListener("click", function () {
      if (!dialog) buildDialog();
      if (dialog.showModal) dialog.showModal();
      else dialog.setAttribute("open", "");
      if (!select.disabled) select.focus();
    });
  })();

  // --- dates: written the way the browser's locale writes them -------------
  //
  // A date field shows and takes the date in the browser's own order and
  // separators, today's date as its placeholder; the calendar button opens
  // the native picker. The native field stays in the form, hidden, and is
  // what is sent, as yyyy-mm-dd.

  (function () {
    var pickers = document.querySelectorAll('form input[type="date"][name]');
    if (!pickers.length || !window.Intl || !Intl.DateTimeFormat.prototype.formatToParts) return;
    var opts = { year: "numeric", month: "2-digit", day: "2-digit" };
    var df;
    try {
      df = new Intl.DateTimeFormat(navigator.language || undefined, opts);
    } catch (err) {
      // a tag Intl does not take (en-US@posix): the default locale instead
      df = new Intl.DateTimeFormat(undefined, opts);
    }
    var order = df.formatToParts(new Date(2000, 10, 22)).map(function (p) { return p.type; })
      .filter(function (t) { return t === "year" || t === "month" || t === "day"; });
    var CAL = '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="1.8" ' +
      'stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="3.5" y="5" width="17" height="15" rx="2"/>' +
      '<path d="M3.5 10h17M8 3v4M16 3v4"/></svg>';

    function pad(n) { return (n < 10 ? "0" : "") + n; }

    function show(iso) {
      var m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(iso || "");
      return m ? df.format(new Date(+m[1], +m[2] - 1, +m[3])) : "";
    }

    // the date typed, as yyyy-mm-dd; "" for none, null for one that is not a date
    function parse(text) {
      text = text.trim();
      if (!text) return "";
      var y, mo, d;
      var iso = /^(\d{4})-(\d{1,2})-(\d{1,2})$/.exec(text);
      if (iso) {
        y = +iso[1]; mo = +iso[2]; d = +iso[3];
      } else {
        var nums = text.match(/\d+/g);
        if (!nums || nums.length !== 3 || order.length !== 3) return null;
        var v = {};
        order.forEach(function (k, i) { v[k] = +nums[i]; });
        y = v.year < 100 ? 2000 + v.year : v.year; mo = v.month; d = v.day;
      }
      var dt = new Date(y, mo - 1, d);
      if (dt.getFullYear() !== y || dt.getMonth() !== mo - 1 || dt.getDate() !== d) return null;
      return y + "-" + pad(mo) + "-" + pad(d);
    }

    Array.prototype.forEach.call(pickers, function (picker) {
      var label = picker.getAttribute("aria-label") || "";
      var wrap = document.createElement("span");
      wrap.className = "date-field";
      var text = document.createElement("input");
      text.type = "text";
      text.className = "date-text";
      text.inputMode = "numeric";
      text.autocomplete = "off";
      text.placeholder = df.format(new Date());
      text.value = show(picker.value);
      if (label) text.setAttribute("aria-label", label);
      if (picker.title) text.title = picker.title;
      var cal = document.createElement("button");
      cal.type = "button";
      cal.className = "date-cal";
      cal.innerHTML = CAL;
      cal.setAttribute("aria-label", label);
      picker.parentNode.insertBefore(wrap, picker);
      wrap.appendChild(text);
      wrap.appendChild(cal);
      wrap.appendChild(picker);
      picker.classList.add("date-native");
      picker.tabIndex = -1;
      picker.setAttribute("aria-hidden", "true");

      cal.addEventListener("click", function () {
        try {
          picker.showPicker();
        } catch (err) {
          picker.focus();
          picker.click();
        }
      });
      picker.addEventListener("change", function () {
        text.value = show(picker.value);
        text.setCustomValidity("");
      });
      text.addEventListener("input", function () {
        var v = parse(text.value);
        if (v === null) {
          text.setCustomValidity(text.placeholder);
        } else {
          text.setCustomValidity("");
          picker.value = v;
        }
      });
    });
  })();

  // --- images ----------------------------------------------------------------

  // every picture of a page opens on its own, full size, in a new tab: the
  // original on wol.jw.org or jw.org it was read from. A picture that already
  // leads somewhere keeps its link; ones streamed in later get theirs as they
  // arrive.
  function linkImages(scope) {
    if (!scope || !scope.querySelectorAll) return;
    var imgs = scope.tagName === "IMG" ? [scope] : scope.querySelectorAll("img");
    Array.prototype.forEach.call(imgs, function (img) {
      if (img.closest("a, button, [data-ui]")) return;
      var src = img.currentSrc || img.getAttribute("src") || "";
      if (!/^https?:\/\//.test(src)) return;
      var a = document.createElement("a");
      a.href = src;
      a.target = "_blank";
      a.rel = "noopener";
      a.className = "img-link";
      if (img.alt) a.title = img.alt;
      img.parentNode.insertBefore(a, img);
      a.appendChild(img);
    });
  }
  var mainEl = document.querySelector("main");
  if (mainEl) {
    linkImages(mainEl);
    if (window.MutationObserver) {
      new MutationObserver(function (records) {
        records.forEach(function (r) {
          Array.prototype.forEach.call(r.addedNodes, function (n) {
            if (n.nodeType === 1) linkImages(n);
          });
        });
      }).observe(mainEl, { childList: true, subtree: true });
    }
  }

  // --- focus ---------------------------------------------------------------

  // the page's main field takes the focus where typing needs no keyboard to
  // open: with a mouse or trackpad. On a touch screen a focused field pops the
  // keyboard up over the page, so there it waits to be tapped.
  var autofocus = document.querySelector("[data-autofocus]");
  if (autofocus && window.matchMedia && window.matchMedia("(hover: hover) and (pointer: fine)").matches) {
    try {
      autofocus.focus({ preventScroll: true });
    } catch (err) {
      autofocus.focus();
    }
  }

  // --- loading ------------------------------------------------------------

  var status = document.querySelector(".loading");
  var busy = null;

  function startLoading(el) {
    root.classList.add("is-loading");
    if (status) status.hidden = false;
    if (el) {
      busy = el;
      el.setAttribute("aria-busy", "true");
    }
  }

  function stopLoading() {
    root.classList.remove("is-loading");
    if (status) status.hidden = true;
    if (busy) {
      busy.removeAttribute("aria-busy");
      busy = null;
    }
  }

  // a form on its way to the server: the submit button spins until the next
  // page replaces this one
  document.addEventListener("submit", function (e) {
    var form = e.target;
    if (!(form instanceof HTMLFormElement) || e.defaultPrevented) return;
    var button = e.submitter || form.querySelector('button[type="submit"], button:not([type])');
    // the next tick, so a submission the browser still rejects (validation)
    // does not leave a spinner behind
    setTimeout(function () {
      if (!e.defaultPrevented) startLoading(button);
    }, 0);
  });

  // a link inside the site: pages that read from jw.org can take a while
  document.addEventListener("click", function (e) {
    if (e.defaultPrevented || e.button !== 0) return;
    if (e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    var a = e.target.closest ? e.target.closest("a[href]") : null;
    if (!a || a.target || a.hasAttribute("download")) return;
    var url;
    try {
      url = new URL(a.href, location.href);
    } catch (err) {
      return;
    }
    if (url.origin !== location.origin) return;
    // files and the API do not replace the page, so they never finish loading
    if (/^\/(download|api|static)\//.test(url.pathname)) return;
    // an anchor on this page is not a load
    if (url.pathname === location.pathname && url.search === location.search && url.hash) return;
    startLoading(a);
  });

  // the page came back from the cache, or the reader stopped the navigation
  window.addEventListener("pageshow", stopLoading);
  window.addEventListener("pagehide", stopLoading);
  document.addEventListener("keydown", function (e) {
    if (e.key === "Escape") stopLoading();
  });

  // --- sections -----------------------------------------------------------

  // an opened section takes a row of its own; keep its title in sight when
  // that moves it. Only for a section the reader opened: one opened by the
  // page putting back what it had must not move the page from where it was
  var touched = 0;
  ["pointerdown", "keydown"].forEach(function (type) {
    document.addEventListener(type, function (e) {
      if (e.target && e.target.closest && e.target.closest("summary")) touched = Date.now();
    }, true);
  });
  document.addEventListener("toggle", function (e) {
    var d = e.target;
    if (!(d instanceof HTMLElement) || !d.matches("details.section") || !d.open) return;
    if (Date.now() - touched > 1500) return;
    var s = d.querySelector(":scope > summary");
    if (!s || !s.getBoundingClientRect) return;
    var r = s.getBoundingClientRect();
    if (r.top < 0 || r.bottom > window.innerHeight) s.scrollIntoView({ block: "nearest" });
  }, true);

  var T = {};
  try {
    T = JSON.parse(document.getElementById("ui-text").textContent) || {};
  } catch (err) {
    T = {};
  }

  // --- remembering where the reader was -------------------------------------
  //
  // Per section and language, the last page read: the menu and the start page
  // lead back to it. Per page, what was on screen — the verses and paragraphs
  // unfolded and what they brought, what was open, how far down — kept in
  // IndexedDB, so coming back shows it at once instead of asking the server
  // again. All of it stays in this browser; storage that is unavailable or
  // full only means nothing is remembered.

  var SECTIONS = [
    ["bible", /^\/bible$/, function (q) { return !!(q.get("ref") || "").trim(); }],
    ["meetings", /^\/meetings(\/(midweek|weekend))?$/, function () { return true; }],
    ["media", /^\/media(\/(category|item)\/[^/]+)?$/, function () { return true; }],
    ["pub", /^\/pub(\/(library|publication)\/.+)?$/, function () { return true; }]
  ];

  function sectionOf(path) {
    for (var i = 0; i < SECTIONS.length; i++) {
      if (SECTIONS[i][1].test(path)) return SECTIONS[i];
    }
    return null;
  }

  function storageGet(key) {
    try {
      return window.localStorage.getItem(key);
    } catch (err) {
      return null;
    }
  }

  function storageSet(key, value) {
    try {
      window.localStorage.setItem(key, value);
    } catch (err) {
      // private mode or full: nothing is remembered
    }
  }

  // pageKey names a page whatever level it was asked at: the level is part of
  // what is remembered about it, not of which page it is
  function pageKey(u) {
    var q = new URLSearchParams(u.search);
    ["lazy", "force", "unfold", "refresh"].forEach(function (k) { q.delete(k); });
    var pairs = [];
    q.forEach(function (v, k) { pairs.push([k, v]); });
    pairs.sort(function (a, b) { return a[0] < b[0] ? -1 : a[0] > b[0] ? 1 : a[1] < b[1] ? -1 : 1; });
    return u.pathname + "?" + pairs.map(function (p) { return p[0] + "=" + p[1]; }).join("&");
  }

  function lastKey(section, lang) {
    return "jw:last:" + section + ":" + (lang || "");
  }

  var here = sectionOf(location.pathname);
  var pageLang = new URLSearchParams(location.search).get("lang") || "";
  // a page that failed, or asks before spending, is not a place to come back to
  var failed = !!document.querySelector("main .notice, main > .error");
  var PS = { key: null, restoreScroll: function () {} };

  if (here && !failed && here[2](new URLSearchParams(location.search))) {
    PS.key = pageKey(location);
    var scrollKey = "jw:scroll:" + PS.key;
    PS.remember = function () {
      var u = new URL(location.href);
      u.searchParams.delete("force");
      storageSet(lastKey(here[0], pageLang), u.pathname + u.search);
    };
    PS.remember();

    // the page puts its own content back first, then its scroll position
    if ("scrollRestoration" in history) history.scrollRestoration = "manual";
    var scrollTimer = null;
    var saveScroll = function () {
      storageSet(scrollKey, String(Math.round(window.scrollY)));
    };
    window.addEventListener("scroll", function () {
      clearTimeout(scrollTimer);
      scrollTimer = setTimeout(saveScroll, 250);
    }, { passive: true });
    window.addEventListener("pagehide", saveScroll);
    PS.restoreScroll = function () {
      if (location.hash) return;
      var y = parseInt(storageGet(scrollKey), 10);
      if (y > 0) window.scrollTo(0, y);
    };
  }

  // the menu and the start page lead back to the last page of each section
  document.querySelectorAll(".site-nav a[href], .destinations a[href]").forEach(function (a) {
    var u;
    try {
      u = new URL(a.href, location.href);
    } catch (err) {
      return;
    }
    var sec = sectionOf(u.pathname);
    if (!sec || u.origin !== location.origin) return;
    var q = new URLSearchParams(u.search);
    var linkLang = q.get("lang") || "";
    q.delete("lang");
    if (q.toString() !== "") return;
    var last = storageGet(lastKey(sec[0], linkLang));
    if (last && last.charAt(0) === "/") a.href = last;
  });

  // --- bible navigation ----------------------------------------------------

  // reset starts the bible over: the book grid, and the menu leading back to
  // it rather than to the reading that was left
  document.querySelectorAll("a[data-reset]").forEach(function (a) {
    a.addEventListener("click", function () {
      try {
        window.localStorage.removeItem(lastKey(a.getAttribute("data-reset"), pageLang));
      } catch (err) {
        // nothing was remembered
      }
    });
  });

  var bibleForm = document.querySelector("form.bible-form");
  if (bibleForm) {
    var bibleRef = bibleForm.querySelector('input[name="ref"]');
    // on the grid, another edition is another grid: show it at once
    var autoEdition = bibleForm.querySelector("select.edition[data-autosubmit]");
    if (autoEdition) {
      autoEdition.addEventListener("change", function () {
        if (bibleRef && bibleRef.value.trim() !== "") return;
        if (bibleForm.requestSubmit) bibleForm.requestSubmit();
        else bibleForm.submit();
      });
    }
    // a reference asked for replaces the grid: it goes as soon as it is sent,
    // and the book it was opened on does not ride along
    bibleForm.addEventListener("submit", function () {
      if (!bibleRef || bibleRef.value.trim() === "") return;
      bibleForm.querySelectorAll("[data-nav-only]").forEach(function (el) { el.disabled = true; });
      document.querySelectorAll(".bible-nav").forEach(function (el) { el.hidden = true; });
    });
    // back to this page out of the browser's cache: the grid is still the page
    window.addEventListener("pageshow", function (e) {
      if (!e.persisted) return;
      bibleForm.querySelectorAll("[data-nav-only]").forEach(function (el) { el.disabled = false; });
      document.querySelectorAll(".bible-nav").forEach(function (el) { el.hidden = false; });
    });
  }

  // page states: one record per page, dropped after a month unvisited
  var STATE_MAX_AGE = 30 * 24 * 3600 * 1000;
  var dbPromise = null;
  // the open database, once it is: the last save of a page that is being left
  // has to start before the page goes, with no promise to wait on first
  var dbHandle = null;

  function openDB() {
    if (!dbPromise) {
      dbPromise = new Promise(function (resolve) {
        try {
          var req = window.indexedDB.open("jw-serve", 1);
          req.onupgradeneeded = function () { req.result.createObjectStore("pages"); };
          req.onsuccess = function () {
            dbHandle = req.result;
            resolve(req.result);
          };
          req.onerror = function () { resolve(null); };
          req.onblocked = function () { resolve(null); };
        } catch (err) {
          resolve(null);
        }
      });
    }
    return dbPromise;
  }

  PS.load = function () {
    if (!PS.key) return Promise.resolve(null);
    return openDB().then(function (db) {
      if (!db) return null;
      return new Promise(function (resolve) {
        try {
          var req = db.transaction("pages").objectStore("pages").get(PS.key);
          req.onsuccess = function () {
            var v = req.result;
            resolve(v && Date.now() - v.t < STATE_MAX_AGE ? v : null);
          };
          req.onerror = function () { resolve(null); };
        } catch (err) {
          resolve(null);
        }
      });
    });
  };

  PS.save = function (value) {
    if (!PS.key) return;
    value.t = Date.now();
    var put = function (db) {
      if (!db) return;
      try {
        db.transaction("pages", "readwrite").objectStore("pages").put(value, PS.key);
      } catch (err) {
        // quota or a closed database: this page is simply not remembered
      }
    };
    if (dbHandle) put(dbHandle);
    else openDB().then(put);
  };

  // once per page load, the records of pages not seen for a month go
  if (PS.key) {
    openDB();
    setTimeout(function () {
      openDB().then(function (db) {
        if (!db) return;
        try {
          var req = db.transaction("pages", "readwrite").objectStore("pages").openCursor();
          req.onsuccess = function () {
            var c = req.result;
            if (!c) return;
            if (!c.value || Date.now() - c.value.t > STATE_MAX_AGE) c.delete();
            c.continue();
          };
        } catch (err) {
          // nothing to sweep
        }
      });
    }, 3000);
  }

  // --- unfolding ----------------------------------------------------------

  // carousels on the media start page: the track scrolls on its own (swipe,
  // trackpad, keyboard); the buttons page it for a mouse and show only while
  // there is somewhere to go
  document.querySelectorAll("[data-carousel]").forEach(function (car) {
    var track = car.querySelector(".car-track");
    var prev = car.querySelector(".car-btn.prev");
    var next = car.querySelector(".car-btn.next");
    if (!track || !prev || !next) return;
    function sync() {
      var max = track.scrollWidth - track.clientWidth;
      prev.hidden = track.scrollLeft <= 1;
      next.hidden = track.scrollLeft >= max - 1;
    }
    function page(dir) {
      track.scrollBy({ left: dir * track.clientWidth * 0.9, behavior: "smooth" });
    }
    prev.addEventListener("click", function () { page(-1); });
    next.addEventListener("click", function () { page(1); });
    track.addEventListener("scroll", sync, { passive: true });
    window.addEventListener("resize", sync);
    sync();
  });

  var doc = document.querySelector(".document[data-unfold]");
  if (!doc || !window.fetch) {
    PS.restoreScroll();
    return;
  }

  // fmt fills %d and %s in order, as the catalogs write them
  function fmt(s) {
    var args = Array.prototype.slice.call(arguments, 1);
    var i = 0;
    return String(s || "").replace(/%[ds]/g, function () {
      return i < args.length ? String(args[i++]) : "";
    });
  }

  function make(tag, cls, text) {
    var el = document.createElement(tag);
    if (cls) el.className = cls;
    if (text != null) el.textContent = text;
    return el;
  }

  function button(cls, label, text) {
    var b = make("button", cls, text);
    b.type = "button";
    b.setAttribute("data-ui", "");
    if (label) {
      b.setAttribute("aria-label", label);
      b.title = label;
    }
    return b;
  }

  var MAX_DEPTH = 3;
  var mode = doc.getAttribute("data-unfold");
  var pageLevel = parseInt(doc.getAttribute("data-level"), 10) || 0;
  var lang = T.lang || new URLSearchParams(location.search).get("lang") || "";

  // the diamond wol marks its study material with: a cut gem, outline and facets
  var ICON = '<svg viewBox="0 0 16 16" width="17" height="17" aria-hidden="true" focusable="false">' +
    '<path fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round" ' +
    'd="M4.2 2.5h7.6L15 6.2 8 14 1 6.2zM1 6.2h14M5.9 2.5 5 6.2 8 14l3-7.8-.9-3.7M5 6.2 8 2.5l3 3.7"/></svg>';

  // --- the items: a verse, or a block of a document citing something -----

  var items = [];

  // addItem makes el unfoldable. A nested item sits inside what another item
  // brought: it unfolds on its own, and is left out of "unfold all".
  function addItem(el, host, params, nested) {
    var item = { el: el, host: host, params: params, level: 0, state: "idle", exp: null, ctrl: null };
    el._unfold = item;
    el.classList.add("unfold-item");
    if (nested) el.classList.add("nested");
    var b = button("unfold-btn", T.unfoldItem || "Unfold");
    b.innerHTML = ICON + '<span class="lvl" aria-hidden="true"></span>';
    b.setAttribute("aria-haspopup", "true");
    b.setAttribute("aria-expanded", "false");
    b.addEventListener("click", function (e) {
      e.preventDefault();
      e.stopPropagation();
      toggleMenu(item);
    });
    el.appendChild(b);
    item.btn = b;
    // what the server already unfolded, on a page asked for without lazy=1
    var exp = existingExpansion(item);
    if (exp) {
      item.exp = exp;
      setLevel(item, parseInt(el.getAttribute("data-level"), 10) || pageLevel || 1);
    }
    if (!nested) items.push(item);
    return item;
  }

  // the div.expansion an item carries, which sits after a block and inside a
  // verse or a list item
  function existingExpansion(item) {
    if (item.host === item.el) {
      return item.el.querySelector(":scope > .expansion");
    }
    var next = item.el.nextElementSibling;
    return next && next.classList.contains("expansion") ? next : null;
  }

  function placeExpansion(item, exp) {
    if (item.host === item.el) {
      item.el.insertBefore(exp, item.btn);
    } else {
      item.el.insertAdjacentElement("afterend", exp);
    }
  }

  var BLOCKS = "p, li, blockquote, figcaption, dd, dt, h1, h2, h3, h4, h5, h6";

  if (mode === "verse") {
    doc.querySelectorAll(".item[data-vid]").forEach(function (el) {
      var passage = el.closest(".passage");
      addItem(el, el, {
        kind: "verse",
        vid: el.getAttribute("data-vid"),
        bible: passage ? passage.getAttribute("data-bible") : ""
      });
    });
  } else {
    citingBlocks(doc, false);
  }

  // titleLinks makes a section headed by a link — a publication quoting a
  // verse — open and close on its whole title like any other: the title is
  // plain text, and the link moves to a small button at the summary's end
  // that does what following it did
  function titleLinks(root) {
    var heads = root.matches && root.matches("details.section") ? [root] : [];
    root.querySelectorAll("details.section").forEach(function (d) { heads.push(d); });
    heads.forEach(function (d) {
      var s = d.querySelector(":scope > summary");
      var a = s && s.querySelector(":scope > a[href]:not(.follow)");
      if (!a) return;
      var target = linkTarget(a);
      if (!target || (target.kind !== "article" && target.kind !== "media")) return;
      var title = make("span", "title");
      while (a.firstChild) title.appendChild(a.firstChild);
      s.replaceChild(title, a);
      a.className = "follow";
      var label = (T.follow || "↗") + ": " + (title.textContent || "").replace(/\s+/g, " ").trim();
      a.setAttribute("aria-label", label);
      a.setAttribute("title", label);
      a.textContent = "↗";
      s.appendChild(a);
    });
  }

  // citingBlocks makes every block of root that cites something an item of
  // its own: a paragraph of the document, or — nested — a paragraph of a
  // passage an unfold brought, so what that cites unfolds in turn.
  function citingBlocks(root, nested) {
    failedRefs(root);
    titleLinks(root);
    var byBlock = new Map();
    root.querySelectorAll('a[href*="/bc/"], a[href*="/pc/"]').forEach(function (a) {
      if (a.closest("summary, [data-ui]")) return;
      if (!nested && a.closest(".expansion, .sections")) return;
      var block = a.closest(BLOCKS);
      if (!block || !root.contains(block) || block.classList.contains("unfold-item")) return;
      if (!nested && block.closest(".expansion")) return;
      var refs = byBlock.get(block);
      if (!refs) {
        refs = [];
        byBlock.set(block, refs);
      }
      var path = a.getAttribute("href");
      if (refs.some(function (r) { return r.path === path; })) return;
      refs.push({ path: path, text: (a.textContent || "").replace(/\s+/g, " ").trim() });
    });
    byBlock.forEach(function (refs, block) {
      // a list item carries its expansion inside itself, any other block
      // right after it
      addItem(block, block.tagName === "LI" ? block : null, { kind: "refs", refs: refs }, nested);
    });
  }

  // what the server already unfolded can be unfolded further, too
  doc.querySelectorAll(".expansion").forEach(function (exp) { citingBlocks(exp, true); });

  if (!items.length) {
    PS.restoreScroll();
    return;
  }
  doc.classList.add("has-unfold");

  // batch is the unfold-all run this item is part of: what it already cost,
  // so the server weighs its budget over the whole run rather than item by
  // item, and its name, under which the server keeps the verses the run
  // showed so that each is shown once
  function streamURL(item, depth, force, batch) {
    var spent = batch ? batch.spent : 0;
    var q = new URLSearchParams();
    if (item.params.kind === "verse") {
      q.set("vid", item.params.vid);
      if (item.params.bible) q.set("bible", item.params.bible);
    }
    if (batch) q.set("run", batch.id);
    if (item.params.kind !== "verse") return refsURL(item.params.refs, depth, force, spent, q);
    return "/unfold/verse?" + unfoldQuery(q, depth, force, spent);
  }

  // refsURL is where the citations refs ({path, text}) unfold from, to depth
  // levels: a citing block, a link followed, or a citation asked for again
  function refsURL(refs, depth, force, spent, q) {
    q = q || new URLSearchParams();
    refs.forEach(function (r) {
      q.append("path", r.path);
      q.append("text", r.text);
    });
    return "/unfold/refs?" + unfoldQuery(q, depth, force, spent);
  }

  // unfoldQuery adds what every unfold stream reads to q: how deep, in which
  // language, and what the run already spent — or that it may spend anyway
  function unfoldQuery(q, depth, force, spent) {
    q.set("depth", String(depth));
    if (lang) q.set("lang", lang);
    if (refreshing) q.set("refresh", "1");
    if (force) q.set("force", "1");
    else if (spent > 0) q.set("spent", String(spent));
    return q.toString();
  }

  // stream reads newline-delimited JSON as it arrives
  function stream(url, onEvent, signal) {
    return fetch(url, { signal: signal, credentials: "same-origin", headers: { Accept: "application/x-ndjson" } })
      .then(function (res) {
        if (!res.ok) {
          return res.json().then(function (j) {
            throw new Error((j && j.error && j.error.message) || res.statusText);
          }, function () {
            throw new Error(res.status + " " + res.statusText);
          });
        }
        var emit = function (line) {
          if (line.trim()) onEvent(JSON.parse(line));
        };
        if (!res.body || !res.body.getReader || !window.TextDecoder) {
          return res.text().then(function (t) { t.split("\n").forEach(emit); });
        }
        var reader = res.body.getReader();
        var dec = new TextDecoder();
        var buf = "";
        var pump = function () {
          return reader.read().then(function (r) {
            if (r.done) {
              emit(buf + dec.decode());
              return;
            }
            buf += dec.decode(r.value, { stream: true });
            var i;
            while ((i = buf.indexOf("\n")) >= 0) {
              emit(buf.slice(0, i));
              buf = buf.slice(i + 1);
            }
            return pump();
          });
        };
        return pump();
      });
  }

  function fromHTML(html) {
    var t = document.createElement("template");
    t.innerHTML = html;
    return t.content.firstElementChild;
  }

  function stageText(stage) {
    switch (stage) {
      case "study": return T.stageStudy || "…";
      case "cited": return T.stageCited || "…";
      default: return T.stageRefs || "…";
    }
  }

  function setLevel(item, level) {
    item.level = level;
    item.btn.querySelector(".lvl").textContent = level > 0 ? String(level) : "";
    item.el.setAttribute("data-level", String(level));
  }

  function setState(item, state) {
    item.state = state;
    syncTools();
    // what an item brought is written at once: a reader may leave the moment
    // it is there, and a write begun as the page goes is not always kept
    if (state !== "loading") writeState();
    item.el.setAttribute("data-state", state);
    if (state === "loading") item.btn.setAttribute("aria-busy", "true");
    else item.btn.removeAttribute("aria-busy");
  }

  function removeExpansion(item) {
    if (item.ctrl) item.ctrl.abort();
    item.ctrl = null;
    if (item.exp) item.exp.remove();
    item.exp = null;
    setLevel(item, 0);
    setState(item, "idle");
  }

  function hint(item, text) {
    var old = item.el.querySelector(":scope > .unfold-hint");
    if (old) old.remove();
    var h = make("span", "unfold-hint", text);
    h.setAttribute("role", "status");
    item.el.appendChild(h);
    setTimeout(function () { h.classList.add("gone"); }, 2200);
    setTimeout(function () { h.remove(); }, 2700);
  }

  // countOf keeps the number of entries a section holds in its summary
  function countOf(section) {
    var list = section.querySelector(":scope > .section-body > .sections");
    var summary = section.querySelector(":scope > summary");
    if (!list || !summary || !onlyList(section)) return;
    var badge = summary.querySelector(":scope > .count");
    if (!badge) {
      badge = make("span", "count");
      summary.appendChild(document.createTextNode(" "));
      summary.appendChild(badge);
    }
    badge.textContent = String(list.children.length);
  }

  // onlyList reports whether a section holds nothing but the list others are
  // streamed into — a verse's references — rather than a passage of its own
  // with its sections after it
  function onlyList(section) {
    var body = section.querySelector(":scope > .section-body");
    return !!body && body.children.length === 1 && body.firstElementChild.classList.contains("sections");
  }

  function makeLoader() {
    var loader = make("div", "unfold-loader");
    loader.innerHTML = '<span class="spinner" aria-hidden="true"></span><span class="text"></span>' +
      '<span class="meter" aria-hidden="true"><i></i></span>';
    loader.querySelector(".text").textContent = T.loading || "…";
    // what is loading can be called off, keeping what already came
    var abort = button("small ghost abort", T.abort || "×", T.abort || "×");
    loader.appendChild(abort);
    return loader;
  }

  // streamSections reads a stream into list, in front of loader: each section
  // as it arrives, in the order the sections read in, the ones streamed into
  // a group into that group. It settles with what the stream said: how many
  // sections came, and whether it was expensive, failed or aborted.
  //
  // The loader's abort button stops the stream where it is: what came stays,
  // and the result says it was stopped rather than aborted (which is what a
  // caller replacing the stream does through signal).
  function streamSections(url, list, loader, signal) {
    var res = { count: 0, expensive: null, failure: "", note: "", requests: 0, aborted: false, stopped: false };
    var groups = {};
    var stage = "";
    var inner = window.AbortController ? new AbortController() : null;
    if (inner && signal) {
      if (signal.aborted) inner.abort();
      else signal.addEventListener("abort", function () { inner.abort(); });
    }
    var abortBtn = loader.querySelector(".abort");
    if (abortBtn) {
      if (!inner) abortBtn.hidden = true;
      abortBtn.addEventListener("click", function (e) {
        e.preventDefault();
        e.stopPropagation();
        res.stopped = true;
        inner.abort();
      });
    }
    var sig = inner ? inner.signal : signal;
    function onEvent(ev) {
      if (sig && sig.aborted) return;
      switch (ev.type) {
        case "stage":
          stage = stageText(ev.stage);
          loader.querySelector(".text").textContent = stage;
          loader.querySelector(".meter i").style.width = "0";
          break;
        case "progress":
          // short enough for a phone: the stage, then how far its level got
          loader.querySelector(".text").textContent = (stage ? stage + " " : "") + ev.done + "/" + ev.total;
          loader.title = fmt(T.progressLevel, ev.level, ev.done, ev.total);
          loader.querySelector(".meter i").style.width = Math.round(100 * ev.done / Math.max(ev.total, 1)) + "%";
          break;
        case "section":
          var node = fromHTML(ev.html);
          if (!node) break;
          node.classList.add("fresh");
          // in the order the sections read in, whenever they arrive
          var into = ev.in && groups[ev.in];
          var target = into ? into.list : list;
          if (ev.unwrap) {
            // the body of a section the page shows already: what it holds
            // goes in ahead of the list its own sections are streamed into
            citingBlocks(node, true);
            var got = node.querySelector(":scope > .section-body");
            var frag = document.createDocumentFragment();
            while (got && got.firstChild) frag.appendChild(got.firstChild);
            if (target.parentNode && target.parentNode.classList.contains("section-body")) {
              target.parentNode.insertBefore(frag, target);
            } else {
              target.insertBefore(frag, loader.parentNode === target ? loader : null);
            }
            res.count++;
            break;
          }
          // a section the list holds already — one a followed link brought,
          // say — is not shown twice
          var ref = node.getAttribute("data-ref");
          if (ref && target.querySelector(":scope > [data-ref=" + cssString(ref) + "]")) break;
          var order = ev.order || 0;
          node.setAttribute("data-order", String(order));
          var before = loader.parentNode === target ? loader : null;
          for (var c = target.firstElementChild; c && c !== loader; c = c.nextElementSibling) {
            if ((parseInt(c.getAttribute("data-order"), 10) || 0) > order) {
              before = c;
              break;
            }
          }
          target.insertBefore(node, before);
          if (into) countOf(into.section);
          if (ev.key) {
            var inner = node.querySelector('.sections[data-key="' + ev.key + '"]');
            if (inner) groups[ev.key] = { list: inner, section: node };
          }
          citingBlocks(node, true);
          res.count++;
          break;
        case "expensive":
          res.expensive = ev;
          break;
        case "error":
          res.failure = ev.text || "";
          break;
        case "done":
          res.note = ev.text || "";
          res.requests = ev.requests || 0;
          break;
      }
    }
    return stream(url, onEvent, sig)
      .catch(function (err) {
        if (err && err.name === "AbortError") res.aborted = !res.stopped;
        else res.failure = (err && err.message) || String(err);
      })
      .then(function () {
        if (res.aborted) return res;
        if (res.stopped) {
          var note = make("p", "note unfold-aborted", T.aborted || "×");
          loader.replaceWith(note);
        }
        loader.remove();
        // empty groups that never got an entry say nothing
        Object.keys(groups).forEach(function (k) {
          if (!groups[k].list.children.length && onlyList(groups[k].section)) {
            groups[k].section.remove();
            res.count--;
          }
        });
        return res;
      });
  }

  // failedRefs gives every reference of root that could not be read a retry
  // button. The server marks such a reference with the citation it was.
  function failedRefs(root) {
    markSavedFailures(root);
    root.querySelectorAll("p.unfold-failed[data-path]").forEach(function (p) {
      if (p.querySelector(":scope > .retry")) return;
      var b = button("small retry", null, T.retry || "Retry");
      b.addEventListener("click", function (e) {
        e.preventDefault();
        e.stopPropagation();
        retryRef(p);
      });
      p.appendChild(document.createTextNode(" "));
      p.appendChild(b);
    });
  }

  // markSavedFailures marks the failures an expansion kept from before the
  // server marked them itself: the citation is read back out of the message,
  // the text out of the heading it sits under. The message words the error as
  // "… GET https://wol.jw.org/…/bc/…: HTTP 403".
  function markSavedFailures(root) {
    var FAILED_GET = /\bGET (\S+\/wol\/(?:bc|pc)\/\S+?):? HTTP \d+/;
    root.querySelectorAll("p:not(.unfold-failed) > em:only-child").forEach(function (em) {
      var m = FAILED_GET.exec(em.textContent || "");
      if (!m) return;
      var path;
      try {
        path = new URL(m[1]).pathname;
      } catch (err) {
        return;
      }
      var p = em.parentElement;
      var d = p.closest("details");
      var summary = d && d.querySelector(":scope > summary");
      p.classList.add("unfold-failed");
      p.setAttribute("data-path", path);
      p.setAttribute("data-text", summary ? (summary.textContent || "").replace(/\s+/g, " ").trim() : "");
    });
  }

  // retryRef asks for one failed reference again, one level deep — what it
  // cites in turn unfolds from its own blocks — and puts what came in place of
  // the message, under the heading the reference already has.
  function retryRef(p) {
    var url = refsURL([{ path: p.getAttribute("data-path"), text: p.getAttribute("data-text") || "" }], 1);
    var list = make("div", "sections");
    var loader = makeLoader();
    list.appendChild(loader);
    p.replaceWith(list);
    streamSections(url, list, loader, null).then(function (res) {
      var sections = list.querySelectorAll(":scope > details.section");
      if (res.failure || !sections.length) {
        // still nothing: the message again, saying what went wrong this time
        if (res.failure) p.querySelector("em").textContent = fmt(T.error, res.failure);
        list.replaceWith(p);
        return;
      }
      // the section repeats the heading this reference already sits under:
      // only what it holds is kept
      var got = document.createDocumentFragment();
      sections.forEach(function (sec) {
        var body = sec.querySelector(":scope > .section-body");
        while (body && body.firstChild) got.appendChild(body.firstChild);
      });
      list.replaceWith(got);
      saveState();
    });
  }

  // unfold loads what one item references, to depth levels, into a fresh
  // expansion under it. opts.force spends an expensive level without asking;
  // opts.batch is the unfold-all run it is part of.
  function unfold(item, depth, opts) {
    opts = opts || {};
    closeMenu();
    if (item.ctrl) item.ctrl.abort();
    var ctrl = window.AbortController ? new AbortController() : null;
    item.ctrl = ctrl;
    if (item.exp) item.exp.remove();

    var exp = make("div", "expansion");
    exp.setAttribute("aria-live", "polite");
    var list = make("div", "sections");
    var loader = makeLoader();
    list.appendChild(loader);
    exp.appendChild(list);
    placeExpansion(item, exp);
    item.exp = exp;
    setState(item, "loading");

    var url = streamURL(item, depth, opts.force, opts.batch);
    return streamSections(url, list, loader, ctrl && ctrl.signal).then(function (res) {
      if (res.aborted || item.ctrl !== ctrl) return;
      item.ctrl = null;
      if (opts.batch) opts.batch.spent += res.requests;
      if (res.expensive) return settleExpensive(item, depth, opts, res.expensive, list, res.count);
      if (res.failure) {
        var msg = make("div", "unfold-msg error", fmt(T.error, res.failure) + " ");
        var retry = button("small", null, T.retry || "Retry");
        retry.addEventListener("click", function () { unfold(item, depth, opts); });
        msg.appendChild(retry);
        exp.appendChild(msg);
        setState(item, "error");
        setLevel(item, 0);
        return;
      }
      if (res.note) exp.appendChild(make("p", "note", res.note));
      if (res.stopped) {
        // called off: what came stays, and the item can be unfolded again
        if (res.count <= 0) {
          exp.remove();
          item.exp = null;
          setLevel(item, 0);
          setState(item, "idle");
          return;
        }
        setLevel(item, depth);
        setState(item, "error");
        return;
      }
      if (res.count <= 0 && !res.note) {
        exp.remove();
        item.exp = null;
        setLevel(item, 0);
        setState(item, "empty");
        if (!opts.batch) hint(item, T.nothing || "∅");
        return;
      }
      setLevel(item, depth);
      setState(item, "done");
    });
  }

  // an expensive level is spent only when the reader says so: once for a whole
  // unfold-all run, and otherwise per item
  function settleExpensive(item, depth, opts, ev, list, count) {
    var batch = opts.batch;
    if (batch && batch.force) {
      return unfold(item, depth, { batch: batch, force: true });
    }
    if (!batch || !batch.asked) {
      if (batch) batch.asked = true;
      if (window.confirm((T.expensiveTitle || "") + "\n\n" + ev.text)) {
        if (batch) batch.force = true;
        return unfold(item, depth, { batch: batch, force: true });
      }
    }
    var msg = make("div", "unfold-msg", ev.text + " ");
    var go = button("small", null, T.unfoldAnyway || "OK");
    go.addEventListener("click", function () { unfold(item, depth, { force: true }); });
    msg.appendChild(go);
    item.exp.appendChild(msg);
    setLevel(item, count > 0 ? depth : 0);
    setState(item, "error");
  }

  // runID names an unfold-all run to the server: random, and new every run
  function runID() {
    var a = new Uint8Array(12);
    if (window.crypto && crypto.getRandomValues) crypto.getRandomValues(a);
    else for (var i = 0; i < a.length; i++) a[i] = Math.floor(Math.random() * 256);
    return Array.prototype.map.call(a, function (b) { return (b < 16 ? "0" : "") + b.toString(16); }).join("");
  }

  // --- the menu of one item: how deep to unfold it ------------------------

  var menu = make("div", "unfold-menu");
  menu.setAttribute("role", "group");
  menu.setAttribute("aria-label", T.unfoldItem || "Unfold");
  menu.hidden = true;
  menu.setAttribute("data-ui", "");
  menu.appendChild(make("span", "label", T.depth || "Depth"));
  for (var d = 1; d <= MAX_DEPTH; d++) {
    (function (depth) {
      var b = button("depth", fmt(T.depthN, depth), String(depth));
      b.setAttribute("data-depth", String(depth));
      b.addEventListener("click", function (e) {
        e.stopPropagation();
        var item = menu.item;
        if (!item) return;
        dequeue(item);
        unfold(item, depth);
      });
      menu.appendChild(b);
    })(d);
  }
  var remove = button("remove", T.foldAway || "×", "×");
  remove.addEventListener("click", function (e) {
    e.stopPropagation();
    var item = menu.item;
    closeMenu();
    if (item) {
      dequeue(item);
      removeExpansion(item);
      item.btn.focus();
    }
  });
  menu.appendChild(remove);

  function toggleMenu(item) {
    if (menu.item === item && !menu.hidden) {
      closeMenu();
      return;
    }
    closeMenu();
    menu.item = item;
    item.el.appendChild(menu);
    menu.querySelectorAll("button.depth").forEach(function (b) {
      b.setAttribute("aria-pressed", String(parseInt(b.getAttribute("data-depth"), 10) === item.level));
    });
    remove.hidden = !item.exp && item.state !== "loading";
    menu.hidden = false;
    item.btn.setAttribute("aria-expanded", "true");
    var first = menu.querySelector('button.depth[aria-pressed="true"]') || menu.querySelector("button.depth");
    if (first) first.focus({ preventScroll: true });
  }

  function closeMenu() {
    if (menu.item) menu.item.btn.setAttribute("aria-expanded", "false");
    menu.hidden = true;
    menu.item = null;
  }

  document.addEventListener("click", function (e) {
    if (!menu.hidden && !menu.contains(e.target)) closeMenu();
  });
  document.addEventListener("keydown", function (e) {
    if (e.key !== "Escape" || menu.hidden) return;
    var item = menu.item;
    closeMenu();
    if (item) item.btn.focus();
  });

  // --- unfold all ---------------------------------------------------------

  var bar = document.querySelector(".unfold-bar");
  var levelLinks = bar ? bar.querySelectorAll(".tabs.unfold a[data-level]") : [];
  var status = make("div", "unfold-status");
  status.hidden = true;
  status.setAttribute("role", "status");
  status.innerHTML = '<span class="spinner" aria-hidden="true"></span><span class="text"></span>' +
    '<span class="meter" aria-hidden="true"><i></i></span>';
  var stop = button("small stop", null, T.stop || "Stop");
  status.appendChild(stop);
  var tools = make("div", "unfold-tools");
  var openAll = button("small pill", null, T.openAll || "Open all");
  var closeAll = button("small pill", null, T.closeAll || "Close all");
  tools.appendChild(openAll);
  tools.appendChild(closeAll);
  if (bar) {
    bar.appendChild(status);
    bar.appendChild(tools);
  }

  // open and close all only once there is something to open
  function syncTools() {
    if (!tools) return;
    tools.hidden = !doc.querySelector("details.section");
  }

  openAll.addEventListener("click", function () {
    doc.querySelectorAll("details.section").forEach(function (d) { d.open = true; });
  });
  closeAll.addEventListener("click", function () {
    doc.querySelectorAll("details.section").forEach(function (d) { d.open = false; });
  });

  var batch = null;
  var queue = [];
  // a reload past the cache asks the server past it until its run is over
  var refreshing = REFRESH;
  // the level the page is unfolded to: what a followed link unfolds to as well
  var anchorLevel = pageLevel;

  function dequeue(item) {
    var i = queue.indexOf(item);
    if (i >= 0) {
      queue.splice(i, 1);
      if (batch) batch.total--;
      showStatus();
    }
  }

  function showStatus() {
    if (!batch) {
      status.hidden = true;
      if (bar) bar.classList.remove("busy");
      if (allBtn) allBtn.classList.remove("busy");
      return;
    }
    status.hidden = false;
    if (bar) bar.classList.add("busy");
    if (allBtn) allBtn.classList.add("busy");
    status.querySelector(".text").textContent =
      (T.unfolding || "") + " " + fmt(T.progressItems, Math.min(batch.done + 1, batch.total), batch.total);
    status.querySelector(".meter i").style.width = Math.round(100 * batch.done / Math.max(batch.total, 1)) + "%";
  }

  // the page bar's diamond unfolds everything, the way an item's diamond
  // unfolds one item: a menu of depths, and × to fold it all away. The level
  // links under the heading stay for a browser without a script.
  var allBtn = null;
  var allMenu = null;
  var pb = document.querySelector(".page-bar .pb");
  if (bar && levelLinks.length && pb) {
    allBtn = button("unfold-btn pb-unfold", T.unfoldAll || "Unfold all");
    allBtn.innerHTML = ICON + '<span class="lvl" aria-hidden="true"></span>';
    allBtn.setAttribute("aria-haspopup", "true");
    allBtn.setAttribute("aria-expanded", "false");
    allMenu = make("div", "unfold-menu pb-unfold-menu");
    allMenu.setAttribute("role", "group");
    allMenu.setAttribute("aria-label", T.unfoldAll || "Unfold all");
    allMenu.setAttribute("data-ui", "");
    allMenu.hidden = true;
    allMenu.appendChild(make("span", "label", T.unfoldAll || "Unfold all"));
    for (var ad = 1; ad <= MAX_DEPTH; ad++) {
      (function (depth) {
        var b = button("depth", fmt(T.depthN, depth), String(depth));
        b.setAttribute("data-depth", String(depth));
        b.addEventListener("click", function (e) {
          e.stopPropagation();
          closeAllMenu();
          unfoldAll(depth);
        });
        allMenu.appendChild(b);
      })(ad);
    }
    var allRemove = button("remove", T.foldAway || "×", "×");
    allRemove.addEventListener("click", function (e) {
      e.stopPropagation();
      closeAllMenu();
      unfoldAll(0);
      allBtn.focus();
    });
    allMenu.appendChild(allRemove);
    var font = pb.querySelector(".pb-font");
    pb.insertBefore(allBtn, font);
    pb.appendChild(allMenu);
    bar.classList.add("has-pb-unfold");
    if (anchorLevel > 0) {
      allBtn.querySelector(".lvl").textContent = String(anchorLevel);
      allBtn.classList.add("on");
    }

    allBtn.addEventListener("click", function (e) {
      e.preventDefault();
      e.stopPropagation();
      if (!allMenu.hidden) {
        closeAllMenu();
        return;
      }
      closeMenu();
      allMenu.querySelectorAll("button.depth").forEach(function (b) {
        b.setAttribute("aria-pressed", String(parseInt(b.getAttribute("data-depth"), 10) === anchorLevel));
      });
      allRemove.hidden = anchorLevel === 0 && !batch;
      allMenu.hidden = false;
      // under the diamond, its right edge on the diamond's, never past the
      // bar's left edge
      var right = pb.clientWidth - allBtn.offsetLeft - allBtn.offsetWidth;
      allMenu.style.right = Math.max(0, Math.min(right, pb.clientWidth - allMenu.offsetWidth)) + "px";
      allBtn.setAttribute("aria-expanded", "true");
      var first = allMenu.querySelector('button.depth[aria-pressed="true"]') || allMenu.querySelector("button.depth");
      if (first) first.focus({ preventScroll: true });
    });
    document.addEventListener("click", function (e) {
      if (!allMenu.hidden && !allMenu.contains(e.target)) closeAllMenu();
    });
    document.addEventListener("keydown", function (e) {
      if (e.key !== "Escape" || allMenu.hidden) return;
      closeAllMenu();
      allBtn.focus();
    });
  }

  function closeAllMenu() {
    if (!allMenu) return;
    allMenu.hidden = true;
    allBtn.setAttribute("aria-expanded", "false");
  }

  function markLevel(level) {
    anchorLevel = level;
    if (allBtn) {
      allBtn.querySelector(".lvl").textContent = level > 0 ? String(level) : "";
      allBtn.classList.toggle("on", level > 0);
    }
    levelLinks.forEach(function (a) {
      var on = parseInt(a.getAttribute("data-level"), 10) === level;
      a.classList.toggle("active", on);
      if (on) a.setAttribute("aria-current", "true");
      else a.removeAttribute("aria-current");
    });
  }

  // the address keeps the level, so a reload or a shared link unfolds the
  // same way — lazily, after the page is shown
  function rememberLevel(level) {
    var u = new URL(location.href);
    u.searchParams.delete("force");
    if (level > 0) {
      u.searchParams.set("unfold", String(level));
      u.searchParams.set("lazy", "1");
    } else if (mode === "verse" || u.searchParams.has("unfold")) {
      u.searchParams.delete("unfold");
      u.searchParams.delete("lazy");
    }
    history.replaceState(history.state, "", u.pathname + u.search + u.hash);
    if (PS.remember) PS.remember();
    document.querySelectorAll('form input[name="unfold"]').forEach(function (input) {
      input.value = String(level);
    });
    document.querySelectorAll("nav.tabs:not(.unfold) a[href]").forEach(function (a) {
      var t = new URL(a.href, location.href);
      if (t.origin !== location.origin || !t.searchParams.has("unfold") && level === 0) return;
      if (t.pathname !== u.pathname && t.pathname.split("/")[1] !== u.pathname.split("/")[1]) return;
      if (level > 0) {
        t.searchParams.set("unfold", String(level));
        t.searchParams.set("lazy", "1");
      } else {
        t.searchParams.delete("unfold");
        t.searchParams.delete("lazy");
      }
      a.href = t.pathname + t.search;
    });
  }

  function stopAll() {
    if (!batch) return;
    batch.stopped = true;
    queue = [];
    items.forEach(function (item) {
      if (item.state === "loading" && item.batch === batch) removeExpansion(item);
    });
    batch = null;
    showStatus();
  }
  stop.addEventListener("click", stopAll);

  function unfoldAll(level) {
    stopAll();
    closeMenu();
    markLevel(level);
    rememberLevel(level);
    if (level === 0) {
      items.forEach(removeExpansion);
      return;
    }
    var todo = items.filter(function (item) { return item.level !== level || item.state === "error"; });
    runAll(todo.map(function (item) { return { item: item, level: level }; }));
  }

  // runAll unfolds each of todo ({item, level}) as one run, two at a time: the
  // next verse starts while the last one waits on its slowest part, and the
  // server paces wol.jw.org either way. A reload past the cache reads past it
  // until its run is over.
  function runAll(todo) {
    var run = { id: runID(), done: 0, total: 0, spent: 0, force: false, asked: false, stopped: false };
    var levels = new Map();
    todo.forEach(function (t) { levels.set(t.item, t.level); });
    queue = todo.map(function (t) { return t.item; });
    if (!queue.length) {
      refreshing = false;
      return;
    }
    run.total = queue.length;
    batch = run;
    showStatus();
    var worker = function () {
      if (run.stopped || !queue.length) return Promise.resolve();
      var item = queue.shift();
      item.batch = run;
      return unfold(item, levels.get(item), { batch: run }).then(function () {
        item.batch = null;
        if (run.stopped) return;
        run.done++;
        showStatus();
        return worker();
      });
    };
    Promise.all([worker(), worker()]).then(function () {
      refreshing = false;
      if (batch === run) {
        batch = null;
        showStatus();
      }
    });
  }

  levelLinks.forEach(function (a) {
    a.addEventListener("click", function (e) {
      if (e.metaKey || e.ctrlKey || e.shiftKey || e.altKey || e.button !== 0) return;
      e.preventDefault();
      unfoldAll(parseInt(a.getAttribute("data-level"), 10) || 0);
    });
  });

  // forms and links that ask for a level ask for it lazily from here on
  document.querySelectorAll("form").forEach(function (form) {
    if (!form.querySelector('input[name="unfold"]') || form.querySelector('input[name="lazy"]')) return;
    var lazy = make("input");
    lazy.type = "hidden";
    lazy.name = "lazy";
    lazy.value = "1";
    form.appendChild(lazy);
  });

  // --- sections that load once they are opened -------------------------------

  // a lazy section — a verse's other translations — reads its body the first
  // time it is opened, and again after a failure
  function loadLazy(d) {
    var url = d.getAttribute("data-lazy");
    if (!url || d.getAttribute("data-loaded")) return Promise.resolve();
    d.setAttribute("data-loaded", "1");
    // a section the server folded names what to load without the language
    if (lang && !/[?&]lang=/.test(url)) url += (url.indexOf("?") < 0 ? "?" : "&") + "lang=" + encodeURIComponent(lang);
    // who quotes a verse: never the documents the reader got to it through
    if (/^\/unfold\/cited\?/.test(url)) {
      selfDocs(d).forEach(function (id) {
        if (!new RegExp("[?&]self=" + id + "(&|$)").test(url)) url += "&self=" + id;
      });
    }
    var body = d.querySelector(":scope > .section-body");
    if (!body) {
      body = make("div", "section-body");
      d.appendChild(body);
    }
    var list = body.querySelector(":scope > .sections");
    if (!list) list = body.appendChild(make("div", "sections"));
    var loader = makeLoader();
    list.appendChild(loader);
    return streamSections(url, list, loader, null).then(function (res) {
      if (res.stopped) {
        // called off before anything came: opened again, it loads again
        if (res.count <= 0) {
          d.removeAttribute("data-loaded");
          list.textContent = "";
          d.open = false;
        }
      } else if (res.failure) {
        d.removeAttribute("data-loaded");
        body.appendChild(make("div", "unfold-msg error", fmt(T.error, res.failure)));
      } else if (res.count <= 0) {
        // opened and found to hold nothing: it says so for a moment, then
        // the heading goes, and with it a group of sections left with none
        vanish(d, body);
        return;
      }
      saveState();
    });
  }

  doc.addEventListener("toggle", function (e) {
    var d = e.target;
    if (d instanceof HTMLElement && d.matches("details.section[data-lazy]") && d.open) loadLazy(d);
  }, true);

  // vanish shows a section that turned out empty as such, briefly, then
  // fades it out and takes it off the page
  var VANISH_AFTER = 1400, VANISH_FADE = 400;
  function vanish(d, body) {
    d.classList.add("vanishing");
    body.appendChild(make("p", "note empty-note", T.emptySection || T.nothing || "∅"));
    setTimeout(function () { d.classList.add("gone"); }, VANISH_AFTER);
    setTimeout(function () {
      var group = d.parentElement;
      d.remove();
      if (group && group.classList.contains("sections") && !group.children.length) group.remove();
      saveState();
    }, VANISH_AFTER + VANISH_FADE);
  }

  // selfDocs are the documents el was reached through, by docid: the page's
  // own document, and every section around el showing a passage of one.
  var DOC_PATH = /\/wol\/d\/(?:[^/]+\/)*?(\d+)\/?$/;
  var DOC_CLASS = /(?:^|\s)docId-(\d+)(?=\s|$)/g;
  function selfDocs(el) {
    var ids = [];
    function add(id) {
      id = parseInt(id, 10);
      if (id > 0 && ids.indexOf(id) < 0) ids.push(id);
    }
    function fromClass(node) {
      var cls = typeof node.className === "string" ? node.className : "";
      var m;
      DOC_CLASS.lastIndex = 0;
      while ((m = DOC_CLASS.exec(cls))) add(m[1]);
    }
    for (var n = el; n && n !== document.documentElement; n = n.parentElement) {
      fromClass(n);
      add(n.getAttribute("data-doc"));
      var m = DOC_PATH.exec(n.getAttribute("data-ref") || "");
      if (m) add(m[1]);
    }
    // the document the page shows, outside whatever was unfolded into it
    doc.querySelectorAll('[class*="docId-"]').forEach(function (node) {
      if (!node.closest(".expansion")) fromClass(node);
    });
    return ids.slice(0, 16);
  }

  // --- following a link: open what it points at, here ---------------------
  //
  // A marginal reference (+), a footnote (*), a verse number, a bible
  // reference or a link to an article leads to its section under the verse or
  // paragraph it is in rather than away from the page: the section is opened
  // and scrolled to, and loaded — that one reference alone — when it is not
  // there yet. A modified click (a new tab) still follows the link itself.

  function cssString(v) {
    return '"' + String(v).replace(/["\\]/g, "\\$&") + '"';
  }

  // linkTarget says what a link points at, and the key its section carries
  function linkTarget(a) {
    var u;
    try {
      u = new URL(a.getAttribute("href"), location.href);
    } catch (err) {
      return null;
    }
    var path = u.pathname;
    if (/\/wol\/fn\//.test(path)) return { kind: "footnote", key: path, url: u };
    if (/\/wol\/(bc|pc)\//.test(path)) return { kind: "ref", key: path, url: u };
    // a verse number — the chapter number, on a chapter's first verse
    if ((a.classList.contains("vl") || a.classList.contains("cl")) && a.closest(".item[data-vid] > .item-text")) {
      return { kind: "translations", key: "translations", url: u };
    }
    var host = u.hostname.toLowerCase();
    // a video quoting a verse: its player page here, not a document to unfold
    if (/(^|\.)jw\.org$/.test(host) && u.searchParams.get("lank")) {
      return { kind: "media", key: u.searchParams.get("lank"), url: u };
    }
    // a document, or a table-of-contents link ("App. C") wol redirects to one
    if (/\/wol\/(d|tc)\//.test(path) || (/(^|\.)jw\.org$/.test(host) && host !== "wol.jw.org" && path.length > 4)) {
      return { kind: "article", key: path, url: u };
    }
    return null;
  }

  // onThisPage is where a link to a document already on the page points: a
  // box or paragraph of the article itself, say. wol marks the root of each
  // document it renders with its docId, and each block with its paragraph id;
  // the link names the document by id and the paragraphs as #h=71-79 (or #p71).
  // Nothing comes back for any other link, which unfolds as usual.
  function onThisPage(a) {
    var u;
    try {
      u = new URL(a.getAttribute("href"), location.href);
    } catch (err) {
      return null;
    }
    var m = /\/wol\/d\/(?:[^/]+\/)*?(\d+)\/?$/.exec(u.pathname);
    if (!m) return null;
    var cls = ".docId-" + m[1];
    // the copy the link is in first, then the first on the page
    var root = a.closest(cls) || doc.querySelector(cls);
    if (!root) return null;
    var p = /^#(?:h=|p)(\d+)/.exec(u.hash);
    if (!p) return root;
    return root.querySelector('[data-pid="' + p[1] + '"]');
  }

  // ownerOf is what a link's section goes under: the verse it is in, the item
  // (a citing paragraph) it is in, the section whose heading it is, or the
  // block it is in
  function ownerOf(a) {
    var verse = a.closest(".item[data-vid]");
    if (verse && verse._unfold && a.closest(".item-text")) return { item: verse._unfold };
    var summary = a.closest("summary");
    if (summary) return { details: summary.parentElement };
    var block = a.closest(BLOCKS);
    if (!block || !doc.contains(block)) return null;
    if (block._unfold) return { item: block._unfold };
    return { block: block };
  }

  // scopeOf is where an owner's sections are, if it has any yet
  function scopeOf(owner) {
    if (owner.item) return owner.item.exp;
    if (owner.details) return owner.details.querySelector(":scope > .section-body");
    var b = owner.block;
    if (b.tagName === "LI") return b.querySelector(":scope > .expansion");
    var next = b.nextElementSibling;
    return next && next.classList.contains("expansion") ? next : null;
  }

  // listOf is the list an owner's sections go into, made when there is none
  function listOf(owner) {
    var scope = scopeOf(owner);
    if (owner.details) {
      var body = scope || owner.details.appendChild(make("div", "section-body"));
      return body.querySelector(":scope > .sections.linked") || body.appendChild(make("div", "sections linked"));
    }
    if (scope) {
      return scope.querySelector(":scope > .sections") || scope.appendChild(make("div", "sections"));
    }
    var exp = make("div", "expansion");
    var list = exp.appendChild(make("div", "sections"));
    if (owner.item) {
      placeExpansion(owner.item, exp);
      owner.item.exp = exp;
    } else if (owner.block.tagName === "LI") {
      owner.block.appendChild(exp);
    } else {
      owner.block.insertAdjacentElement("afterend", exp);
    }
    return list;
  }

  var reduceMotion = window.matchMedia && window.matchMedia("(prefers-reduced-motion: reduce)").matches;

  // reveal opens el and every section around it, and brings it into view
  function reveal(el) {
    for (var d = el.closest("details"); d; d = d.parentElement && d.parentElement.closest("details")) {
      if (!d.open) d.open = true;
    }
    var at = el.matches("details") ? el.querySelector(":scope > summary") || el : el;
    el.classList.remove("flash");
    void el.offsetWidth;
    el.classList.add("flash");
    at.scrollIntoView({ block: "start", behavior: reduceMotion ? "auto" : "smooth" });
  }

  function findRef(scope, key) {
    return scope ? scope.querySelector("[data-ref=" + cssString(key) + "]") : null;
  }

  // translationsSection is a verse's lazy translations section, made here
  // when the verse was not unfolded: it loads once it is opened
  function translationsSection(item) {
    var q = new URLSearchParams({ vid: item.params.vid, bible: item.params.bible || "nwtsty" });
    if (lang) q.set("lang", lang);
    var d = make("details", "section");
    d.setAttribute("data-ref", "translations");
    d.setAttribute("data-lazy", "/unfold/translations?" + q.toString());
    d.setAttribute("data-order", "55");
    d.appendChild(make("summary", null, T.translations || "…"));
    d.appendChild(make("div", "section-body"));
    return d;
  }

  // anchorDepth is how deep what a link leads to unfolds: as deep as the item
  // it is in, else as the page, and one level when neither is unfolded
  function anchorDepth(owner) {
    var l = owner && owner.item ? owner.item.level : 0;
    if (!(l > 0)) l = anchorLevel;
    return Math.min(Math.max(l || 1, 1), MAX_DEPTH);
  }

  function streamOne(url, list) {
    var loader = makeLoader();
    list.appendChild(loader);
    return streamSections(url, list, loader, null);
  }

  function follow(a, target) {
    var owner = ownerOf(a);
    if (!owner) return false;
    if (owner.details) owner.details.open = true;
    var found = findRef(scopeOf(owner), target.key);
    if (found) {
      reveal(found);
      return true;
    }
    var list = listOf(owner);
    var done = function () {
      if (owner.item) setState(owner.item, "done");
      var el = findRef(scopeOf(owner), target.key);
      if (el) reveal(el);
      else hint(owner.item || { el: a.closest(BLOCKS) || a.parentElement }, T.nothing || "∅");
      saveState();
    };
    var q = new URLSearchParams();
    if (lang) q.set("lang", lang);
    switch (target.kind) {
      case "translations":
        var d = translationsSection(owner.item);
        list.appendChild(d);
        reveal(d);
        setState(owner.item, "done");
        return true;
      case "ref":
        var depth = anchorDepth(owner);
        if (owner.item && owner.item.params.kind === "verse") {
          // a marginal reference of a verse: the verse's references as they
          // unfold, under their heading, and its other sections as headings
          // that load once opened
          q.set("vid", owner.item.params.vid);
          if (owner.item.params.bible) q.set("bible", owner.item.params.bible);
          q.set("part", "marginal");
          q.set("lazy", "1");
          q.set("depth", String(depth));
          streamOne("/unfold/verse?" + q.toString(), list).then(function () {
            if (!(owner.item.level > 0)) setLevel(owner.item, depth);
            done();
          });
          return true;
        }
        var text = (a.textContent || "").replace(/\s+/g, " ").trim();
        streamOne(refsURL([{ path: target.url.pathname, text: text }], depth), list).then(done);
        return true;
      case "footnote":
        q.set("path", target.url.pathname);
        var existing = findRef(scopeOf(owner), "footnotes");
        if (existing && existing.hasAttribute("data-lazy") && !existing.getAttribute("data-loaded")) {
          // the verse's footnotes, not loaded yet: all of them, then this one
          var loading = loadLazy(existing);
          existing.open = true;
          loading.then(done);
          return true;
        }
        if (existing) {
          // another footnote of a verse whose footnotes are out already:
          // into the same section
          var tmp = make("div", "sections");
          var body = existing.querySelector(":scope > .section-body");
          streamOne("/unfold/footnote?" + q.toString(), tmp).then(function () {
            tmp.querySelectorAll(".footnote").forEach(function (f) { body.appendChild(f); });
            done();
          });
        } else {
          streamOne("/unfold/footnote?" + q.toString(), list).then(done);
        }
        return true;
      case "article":
        q.set("url", target.url.href);
        q.set("depth", String(anchorDepth(owner)));
        streamOne("/unfold/article?" + q.toString(), list).then(done);
        return true;
    }
    return false;
  }

  doc.addEventListener("click", function (e) {
    if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    var a = e.target.closest ? e.target.closest("a[href]") : null;
    if (!a || !doc.contains(a) || a.closest("[data-ui]") || a.target) return;
    var here = onThisPage(a);
    if (here) {
      e.preventDefault();
      reveal(here);
      return;
    }
    var target = linkTarget(a);
    if (!target) return;
    e.preventDefault();
    if (target.kind === "media") {
      location.href = "/media/item/" + encodeURIComponent(target.key) + (lang ? "?lang=" + encodeURIComponent(lang) : "");
      return;
    }
    follow(a, target);
  });

  // --- what this page had on screen ---------------------------------------

  // cleanCopy is an expansion as it is kept: what it brought, with the
  // controls this script added taken off again — they are added anew when it
  // comes back. What each nested item was unfolded to stays on it.
  function cleanCopy(exp) {
    var copy = exp.cloneNode(true);
    copy.querySelectorAll("[data-ui], .unfold-menu, .unfold-hint, .unfold-loader, details.vanishing").forEach(function (el) { el.remove(); });
    copy.querySelectorAll(".unfold-item").forEach(function (el) {
      el.classList.remove("unfold-item", "nested");
      el.removeAttribute("data-state");
    });
    copy.querySelectorAll(".fresh, .flash").forEach(function (el) { el.classList.remove("fresh", "flash"); });
    // a lazy section caught loading loads again when it comes back
    copy.querySelectorAll("details[data-lazy][data-loaded]").forEach(function (d) {
      var list = d.querySelector(":scope > .section-body > .sections");
      if (list && !list.children.length) d.removeAttribute("data-loaded");
    });
    return copy.outerHTML;
  }

  // the disclosures of the page outside any expansion, whose open state is
  // kept by position; those inside one carry it in their own markup
  function pageDetails() {
    return Array.prototype.filter.call(doc.querySelectorAll("details"), function (d) {
      return !d.closest(".expansion");
    });
  }

  // signature tells this page's items from another page's under the same
  // address — /meetings is a new week every week — by what they cite
  function signature() {
    var parts = items.map(function (item) {
      return item.params.kind === "verse"
        ? item.params.vid + "@" + (item.params.bible || "")
        : item.params.refs.map(function (r) { return r.path; }).join(",");
    });
    var h = 0;
    var str = parts.join("|");
    for (var i = 0; i < str.length; i++) h = (h * 31 + str.charCodeAt(i)) | 0;
    return items.length + ":" + h;
  }
  var sig = signature();

  var restoring = true;
  var stateTimer = null;

  function saveState() {
    if (!PS.key || restoring) return;
    clearTimeout(stateTimer);
    stateTimer = setTimeout(writeState, 300);
  }

  function writeState() {
    clearTimeout(stateTimer);
    if (!PS.key || restoring) return;
    PS.save({
      n: items.length,
      sig: sig,
      items: items.map(function (item) {
        if (!item.exp || item.state === "loading" || !item.exp.isConnected) return null;
        return { level: item.level, html: cleanCopy(item.exp) };
      }),
      open: pageDetails().map(function (d) { return d.open; })
    });
  }

  document.addEventListener("toggle", function (e) {
    if (doc.contains(e.target)) saveState();
  }, true);
  window.addEventListener("pagehide", writeState);
  document.addEventListener("visibilitychange", function () {
    if (document.visibilityState === "hidden") writeState();
  });

  // restoreState puts back what the page had: an expansion under every item
  // that had one, open what was open. Only for the same page as it was — the
  // same number of items — since a position means nothing on another one.
  function restoreState(saved) {
    if (!saved || saved.sig !== sig || !Array.isArray(saved.items)) return 0;
    if (REFRESH) return refreshState(saved);
    var levels = {};
    saved.items.forEach(function (st, i) {
      var item = items[i];
      if (!st || !item || item.exp || !st.html) return;
      var exp = fromHTML(st.html);
      if (!exp) return;
      placeExpansion(item, exp);
      item.exp = exp;
      setLevel(item, st.level);
      setState(item, "done");
      citingBlocks(exp, true);
      levels[st.level] = true;
    });
    var open = Array.isArray(saved.open) ? saved.open : [];
    pageDetails().forEach(function (d, i) {
      if (i < open.length) d.open = !!open[i];
    });
    // the switcher shows the level that came back, when there was one
    var ls = Object.keys(levels);
    return ls.length === 1 ? parseInt(ls[0], 10) : 0;
  }

  // refreshState is restoreState for a reload past the cache: what was open
  // opens again, and what was unfolded is asked for anew, each item to the
  // level it had, instead of being put back as it was kept
  var refreshTodo = [];
  function refreshState(saved) {
    var levels = {};
    saved.items.forEach(function (st, i) {
      var item = items[i];
      if (!st || !item || item.exp || !(st.level > 0)) return;
      refreshTodo.push({ item: item, level: st.level });
      levels[st.level] = true;
    });
    var open = Array.isArray(saved.open) ? saved.open : [];
    pageDetails().forEach(function (d, i) {
      if (i < open.length) d.open = !!open[i];
    });
    var ls = Object.keys(levels);
    return ls.length === 1 ? parseInt(ls[0], 10) : 0;
  }

  syncTools();
  var auto = parseInt(doc.getAttribute("data-auto"), 10) || 0;
  PS.load().then(function (saved) {
    var restored = restoreState(saved);
    restoring = false;
    syncTools();
    PS.restoreScroll();
    // what the address asks for goes on from what came back: items already
    // at that level are left as they are
    if (auto > 0) {
      unfoldAll(Math.min(auto, MAX_DEPTH));
    } else {
      markLevel(restored || pageLevel);
      runAll(refreshTodo);
    }
  });
})();
