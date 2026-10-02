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

  var header = document.querySelector(".site-header");
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
    ["pub", /^\/pub$/, function (q) { return !!(q.get("pub") || q.get("docid")); }]
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
    ["lazy", "force", "unfold"].forEach(function (k) { q.delete(k); });
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

  // citingBlocks makes every block of root that cites something an item of
  // its own: a paragraph of the document, or — nested — a paragraph of a
  // passage an unfold brought, so what that cites unfolds in turn.
  function citingBlocks(root, nested) {
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

  // spent is what the run this item is part of already cost, so the server
  // weighs its budget over the whole run rather than item by item
  function streamURL(item, depth, force, spent) {
    var q = new URLSearchParams();
    var path;
    if (item.params.kind === "verse") {
      path = "/unfold/verse";
      q.set("vid", item.params.vid);
      if (item.params.bible) q.set("bible", item.params.bible);
    } else {
      path = "/unfold/refs";
      item.params.refs.forEach(function (r) {
        q.append("path", r.path);
        q.append("text", r.text);
      });
    }
    q.set("depth", String(depth));
    if (lang) q.set("lang", lang);
    if (force) q.set("force", "1");
    else if (spent > 0) q.set("spent", String(spent));
    return path + "?" + q.toString();
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
    if (!list || !summary) return;
    var badge = summary.querySelector(":scope > .count");
    if (!badge) {
      badge = make("span", "count");
      summary.appendChild(document.createTextNode(" "));
      summary.appendChild(badge);
    }
    badge.textContent = String(list.children.length);
  }

  function makeLoader() {
    var loader = make("div", "unfold-loader");
    loader.innerHTML = '<span class="spinner" aria-hidden="true"></span><span class="text"></span>' +
      '<span class="meter" aria-hidden="true"><i></i></span>';
    loader.querySelector(".text").textContent = T.loading || "…";
    return loader;
  }

  // streamSections reads a stream into list, in front of loader: each section
  // as it arrives, in the order the sections read in, the ones streamed into
  // a group into that group. It settles with what the stream said: how many
  // sections came, and whether it was expensive, failed or aborted.
  function streamSections(url, list, loader, signal) {
    var res = { count: 0, expensive: null, failure: "", note: "", requests: 0, aborted: false };
    var groups = {};
    var stage = "";
    function onEvent(ev) {
      if (signal && signal.aborted) return;
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
          var into = ev.in && groups[ev.in];
          if (into) {
            into.list.appendChild(node);
            countOf(into.section);
          } else {
            // in the order the sections read in, whenever they arrive
            var order = ev.order || 0;
            node.setAttribute("data-order", String(order));
            var before = loader.parentNode === list ? loader : null;
            for (var c = list.firstElementChild; c && c !== loader; c = c.nextElementSibling) {
              if ((parseInt(c.getAttribute("data-order"), 10) || 0) > order) {
                before = c;
                break;
              }
            }
            list.insertBefore(node, before);
          }
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
    return stream(url, onEvent, signal)
      .catch(function (err) {
        if (err && err.name === "AbortError") res.aborted = true;
        else res.failure = (err && err.message) || String(err);
      })
      .then(function () {
        if (res.aborted) return res;
        loader.remove();
        // empty groups that never got an entry say nothing
        Object.keys(groups).forEach(function (k) {
          if (!groups[k].list.children.length) {
            groups[k].section.remove();
            res.count--;
          }
        });
        return res;
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

    var url = streamURL(item, depth, opts.force, opts.batch ? opts.batch.spent : 0);
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
  var openAll = button("small ghost", null, T.openAll || "Open all");
  var closeAll = button("small ghost", null, T.closeAll || "Close all");
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
      return;
    }
    status.hidden = false;
    if (bar) bar.classList.add("busy");
    status.querySelector(".text").textContent =
      (T.unfolding || "") + " " + fmt(T.progressItems, Math.min(batch.done + 1, batch.total), batch.total);
    status.querySelector(".meter i").style.width = Math.round(100 * batch.done / Math.max(batch.total, 1)) + "%";
  }

  function markLevel(level) {
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
    var run = { level: level, done: 0, total: 0, spent: 0, force: false, asked: false, stopped: false };
    queue = items.filter(function (item) { return item.level !== level || item.state === "error"; });
    if (!queue.length) return;
    run.total = queue.length;
    batch = run;
    showStatus();
    // two at a time: the next verse starts while the last one waits on its
    // slowest part, and the server paces wol.jw.org either way
    var worker = function () {
      if (run.stopped || !queue.length) return Promise.resolve();
      var item = queue.shift();
      item.batch = run;
      return unfold(item, level, { batch: run }).then(function () {
        item.batch = null;
        if (run.stopped) return;
        run.done++;
        showStatus();
        return worker();
      });
    };
    Promise.all([worker(), worker()]).then(function () {
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
      if (res.failure) {
        d.removeAttribute("data-loaded");
        body.appendChild(make("div", "unfold-msg error", fmt(T.error, res.failure)));
      } else if (res.count <= 0) {
        body.appendChild(make("p", "note", T.nothing || "∅"));
      }
      saveState();
    });
  }

  doc.addEventListener("toggle", function (e) {
    var d = e.target;
    if (d instanceof HTMLElement && d.matches("details.section[data-lazy]") && d.open) loadLazy(d);
  }, true);

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
    if (/\/wol\/d\//.test(path) || (/(^|\.)jw\.org$/.test(host) && host !== "wol.jw.org" && path.length > 4)) {
      return { kind: "article", key: path, url: u };
    }
    return null;
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
        q.set("path", target.url.pathname);
        q.set("text", (a.textContent || "").replace(/\s+/g, " ").trim());
        q.set("depth", "1");
        streamOne("/unfold/refs?" + q.toString(), list).then(done);
        return true;
      case "footnote":
        q.set("path", target.url.pathname);
        var existing = findRef(scopeOf(owner), "footnotes");
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
        streamOne("/unfold/article?" + q.toString(), list).then(done);
        return true;
    }
    return false;
  }

  doc.addEventListener("click", function (e) {
    if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    var a = e.target.closest ? e.target.closest("a[href]") : null;
    if (!a || !doc.contains(a) || a.closest("[data-ui]")) return;
    var target = linkTarget(a);
    if (!target) return;
    e.preventDefault();
    follow(a, target);
  });

  // --- what this page had on screen ---------------------------------------

  // cleanCopy is an expansion as it is kept: what it brought, with the
  // controls this script added taken off again — they are added anew when it
  // comes back. What each nested item was unfolded to stays on it.
  function cleanCopy(exp) {
    var copy = exp.cloneNode(true);
    copy.querySelectorAll("[data-ui], .unfold-menu, .unfold-hint, .unfold-loader").forEach(function (el) { el.remove(); });
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
    }
  });
})();
