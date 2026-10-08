// jw serve — the browser's own copy of the site's pages. A page read once
// comes back from here at once, without the server or jw.org, and still
// comes back when neither can be reached: what the reader unfolded on it is
// put back by the page's script, from what it kept itself. Only the bar's
// reload (?refresh=1) asks the server again past this copy, and replaces it.
//
// A page that reads the same whatever the day — an article, a chapter, a
// search — is kept for a month. A page that shows what is current — today's
// text, this week's meetings, the newest videos and issues — only for the
// day it was read, and for a few hours of it.
//
// The worker is stamped with the build serving it: a new build brings a new
// worker, which starts its copy anew, since its pages are written for it.
"use strict";

var BUILD = "__BUILD__";
var PAGES = "jw-pages-" + BUILD;
var STATIC = "jw-static-" + BUILD;
var META = "jw-meta";
var MAX_PAGES = 600;
var HOUR = 3600 * 1000;
var KEEP = 30 * 24 * HOUR;
var KEEP_CURRENT = 6 * HOUR;
var STAMP = "X-JW-Kept-At";

// the pages of the site, as opposed to its API, streams and downloads
var PAGE = /^\/(search|article|bible|dailytext|meetings(\/[^/]+)?|media(\/.*)?|pub(\/.*)?)?$/;

self.addEventListener("install", function (e) {
  e.waitUntil(caches.open(STATIC).then(function (c) {
    return c.addAll(["/static/app.js", "/static/style.css", "/static/icon.svg"]);
  }).catch(function () {}).then(function () { return self.skipWaiting(); }));
});

self.addEventListener("activate", function (e) {
  e.waitUntil(caches.keys().then(function (names) {
    return Promise.all(names.filter(function (n) {
      return n !== PAGES && n !== STATIC && n !== META;
    }).map(function (n) { return caches.delete(n); }));
  }).then(function () { return self.clients.claim(); }));
});

// --- the language a page without ?lang= is read in --------------------------
//
// The server answers such a page in the language the reader last picked,
// which a cookie remembers; the copy kept is the one of that language. The
// worker reads the cookie where the browser lets it, and is told it by every
// page otherwise.

var lang = null;

self.addEventListener("message", function (e) {
  var d = e.data || {};
  if (d.type !== "lang") return;
  lang = String(d.lang || "");
  e.waitUntil(caches.open(META).then(function (c) {
    return c.put("/__lang", new Response(lang));
  }));
});

function currentLang() {
  var kept = function () {
    if (lang !== null) return Promise.resolve(lang);
    return caches.open(META).then(function (c) { return c.match("/__lang"); })
      .then(function (r) { return r ? r.text() : ""; })
      .then(function (v) { return (lang = v); }, function () { return ""; });
  };
  if (!self.cookieStore) return kept();
  return self.cookieStore.get("lang").then(function (c) {
    return c ? decodeURIComponent(c.value) : kept();
  }, kept);
}

// --- pages --------------------------------------------------------------------

// key names a page whatever brought the reader to it: a reload's ?refresh=1
// and the level a page unfolds itself to afterwards (?unfold=…&lazy=1, which
// its script reads off the address) are not part of which page it is
function key(u, lng) {
  var k = new URL(u.href);
  k.hash = "";
  k.searchParams.delete("refresh");
  k.searchParams.delete("nosw");
  if (k.searchParams.get("lazy") === "1") {
    k.searchParams.delete("lazy");
    k.searchParams.delete("unfold");
  }
  if (!k.searchParams.has("lang") && lng) k.searchParams.set("lang", lng);
  k.searchParams.sort();
  return k.origin + k.pathname + "?" + k.searchParams.toString();
}

// current reports a page that shows what is current rather than what was
// asked for by name
function current(u) {
  var p = u.pathname;
  if (p === "/" || p === "/media" || p === "/pub" || /^\/(media\/category|pub\/library)\//.test(p)) return true;
  return (p === "/dailytext" || /^\/meetings/.test(p)) && !u.searchParams.get("date");
}

function fresh(hit, u) {
  var at = parseInt(hit.headers.get(STAMP), 10) || 0;
  var age = Date.now() - at;
  if (!current(u)) return age < KEEP;
  return age < KEEP_CURRENT && new Date(at).toDateString() === new Date().toDateString();
}

// a page is kept when it is one: the page itself, as asked, and not one
// saying something went wrong
function keepable(res) {
  return res.status === 200 && res.type === "basic" && !res.redirected &&
    /^text\/html/.test(res.headers.get("Content-Type") || "") &&
    res.headers.get("X-JW-Keep") !== "no";
}

var puts = 0;

function keep(cache, k, res) {
  return res.blob().then(function (body) {
    var headers = new Headers(res.headers);
    headers.set(STAMP, String(Date.now()));
    return cache.put(k, new Response(body, { status: res.status, statusText: res.statusText, headers: headers }));
  }).then(function () {
    // the oldest copies go once there are too many: the cache lists them in
    // the order they were put
    if (++puts % 20 !== 1) return;
    return cache.keys().then(function (keys) {
      var over = keys.length - MAX_PAGES;
      return Promise.all(keys.slice(0, Math.max(over, 0)).map(function (r) { return cache.delete(r); }));
    });
  }).catch(function () {});
}

// signIn sends a page the server wants a login for past this worker, so the
// browser asks for it the way it does without one
function signIn(u) {
  var to = new URL(u.href);
  to.searchParams.set("nosw", "1");
  return new Response("<!DOCTYPE html><meta charset=\"utf-8\"><script>location.replace(" +
    JSON.stringify(to.pathname + to.search) + ")</script>", { headers: { "Content-Type": "text/html; charset=utf-8" } });
}

function page(e, u) {
  var req = e.request;
  return currentLang().then(function (lng) {
    var k = key(u, lng);
    return caches.open(PAGES).then(function (cache) {
      var fromServer = function () {
        return fetch(req).then(function (res) {
          if (res.status === 401) return signIn(u);
          if (keepable(res)) e.waitUntil(keep(cache, k, res.clone()));
          return res;
        });
      };
      // the bar's reload: past the copy, which the answer replaces
      if (u.searchParams.get("refresh") === "1") {
        return fromServer().catch(function (err) {
          return cache.match(k).then(function (hit) {
            if (hit) return hit;
            throw err;
          });
        });
      }
      return cache.match(k).then(function (hit) {
        if (hit && fresh(hit, u)) return hit;
        // out of date: anew, and the old copy when the server cannot answer
        return fromServer().catch(function (err) {
          if (hit) return hit;
          throw err;
        });
      });
    });
  });
}

// --- the page's own files: the same for as long as the build is ------------

function asset(req) {
  return caches.open(STATIC).then(function (cache) {
    return cache.match(req).then(function (hit) {
      return hit || fetch(req).then(function (res) {
        if (res.ok) cache.put(req, res.clone());
        return res;
      });
    });
  });
}

self.addEventListener("fetch", function (e) {
  var req = e.request;
  if (req.method !== "GET") return;
  var u = new URL(req.url);
  if (u.origin !== self.location.origin) return;
  if (u.pathname.indexOf("/static/") === 0) {
    e.respondWith(asset(req));
    return;
  }
  if (req.mode !== "navigate" || !PAGE.test(u.pathname) || u.searchParams.has("nosw")) return;
  e.respondWith(page(e, u));
});
