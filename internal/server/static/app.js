// jw serve — what the pages need from a script: a menu button on small
// screens, a visible "loading" state while the server talks to jw.org, and the
// unfolding of a document after it is on screen: every verse or paragraph that
// cites something gets a button that loads what it references, section by
// section, as the server finds it. Every page works without it; this only
// makes reading, waiting and navigating nicer.
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
  // that moves it
  document.addEventListener("toggle", function (e) {
    var d = e.target;
    if (!(d instanceof HTMLElement) || !d.matches("details.section") || !d.open) return;
    var s = d.querySelector(":scope > summary");
    if (!s || !s.getBoundingClientRect) return;
    var r = s.getBoundingClientRect();
    if (r.top < 0 || r.bottom > window.innerHeight) s.scrollIntoView({ block: "nearest" });
  }, true);

  // --- unfolding ----------------------------------------------------------

  var doc = document.querySelector(".document[data-unfold]");
  if (!doc || !window.fetch) return;

  var T = {};
  try {
    T = JSON.parse(document.getElementById("ui-text").textContent) || {};
  } catch (err) {
    T = {};
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

  var ICON = '<svg viewBox="0 0 16 16" width="16" height="16" aria-hidden="true" focusable="false">' +
    '<path fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" ' +
    'd="M4 4.5l4 3.5 4-3.5M4 8.5l4 3.5 4-3.5"/></svg>';

  // --- the items: a verse, or a block of a document citing something -----

  var items = [];

  function addItem(el, host, params) {
    var item = { el: el, host: host, params: params, level: 0, state: "idle", exp: null, ctrl: null };
    el.classList.add("unfold-item");
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
    items.push(item);
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
    var byBlock = new Map();
    doc.querySelectorAll('a[href*="/bc/"], a[href*="/pc/"]').forEach(function (a) {
      if (a.closest(".expansion, .sections, [data-ui]")) return;
      var block = a.closest(BLOCKS);
      if (!block || !doc.contains(block) || block.closest(".expansion")) return;
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
      addItem(block, block.tagName === "LI" ? block : null, { kind: "refs", refs: refs });
    });
  }

  if (!items.length) return;
  doc.classList.add("has-unfold");

  function streamURL(item, depth, force) {
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
    var loader = make("div", "unfold-loader");
    loader.innerHTML = '<span class="spinner" aria-hidden="true"></span><span class="text"></span>' +
      '<span class="meter" aria-hidden="true"><i></i></span>';
    loader.querySelector(".text").textContent = T.loading || "…";
    list.appendChild(loader);
    exp.appendChild(list);
    placeExpansion(item, exp);
    item.exp = exp;
    setState(item, "loading");

    var groups = {};
    var count = 0;
    var stage = "";
    var expensive = null;
    var failure = "";
    var note = "";

    function onEvent(ev) {
      if (ctrl && ctrl.signal.aborted) return;
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
            var before = loader;
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
          count++;
          break;
        case "expensive":
          expensive = ev;
          break;
        case "error":
          failure = ev.text || "";
          break;
        case "done":
          note = ev.text || "";
          break;
      }
    }

    return stream(streamURL(item, depth, opts.force), onEvent, ctrl && ctrl.signal)
      .catch(function (err) {
        if (err && err.name === "AbortError") return "aborted";
        failure = (err && err.message) || String(err);
      })
      .then(function (outcome) {
        if (outcome === "aborted" || item.ctrl !== ctrl) return;
        item.ctrl = null;
        loader.remove();
        // empty groups that never got an entry say nothing
        Object.keys(groups).forEach(function (k) {
          if (!groups[k].list.children.length) {
            groups[k].section.remove();
            count--;
          }
        });
        if (expensive) return settleExpensive(item, depth, opts, expensive, list, count);
        if (failure) {
          var msg = make("div", "unfold-msg error", fmt(T.error, failure) + " ");
          var retry = button("small", null, T.retry || "Retry");
          retry.addEventListener("click", function () { unfold(item, depth, opts); });
          msg.appendChild(retry);
          exp.appendChild(msg);
          setState(item, "error");
          setLevel(item, 0);
          return;
        }
        if (note) exp.appendChild(make("p", "note", note));
        if (count <= 0 && !note) {
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
    var run = { level: level, done: 0, total: 0, force: false, asked: false, stopped: false };
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

  syncTools();
  var auto = parseInt(doc.getAttribute("data-auto"), 10) || 0;
  if (auto > 0) {
    unfoldAll(Math.min(auto, MAX_DEPTH));
  } else {
    markLevel(pageLevel);
  }
})();
