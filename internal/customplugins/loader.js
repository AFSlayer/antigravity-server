// Loads the user's custom plugins into the web app and draws their section in Settings > Appearance. This file is the same
// for every page and kept by the browser; it asks for the enabled plugins without holding up the page. The styles that
// were on last time are put in at once, so a theme does not flash the app's own colours first, and are set right once
// the list arrives. Opening the
// app with ?agy-safe=1 loads none of them for the rest of the tab's life, so a plugin that breaks the page can still
// be turned off from the same section; ?agy-safe=0 ends it.
(function () {
  "use strict";
  var SAFE_KEY = "agyCustomPluginsSafe";
  var LAST_DIR_KEY = "agyCustomPluginsLastDir";
  var HIDDEN_KEY = "agyCustomPluginsShowHidden";
  var STYLES_KEY = "agyCustomPluginsStyles";
  var safe = false;
  try {
    var asked = new URLSearchParams(location.search).get("agy-safe");
    if (asked !== null) {
      if (/^(0|false|no|off)$/i.test(asked)) sessionStorage.removeItem(SAFE_KEY);
      else sessionStorage.setItem(SAFE_KEY, "1");
    }
    safe = sessionStorage.getItem(SAFE_KEY) === "1";
  } catch (e) {}
  window.__agyCustomPluginsSafe = safe;
  window.__agyCustomPluginsLoaded = [];

  function store(key, value) { try { if (value === null) localStorage.removeItem(key); else localStorage.setItem(key, value); } catch (e) {} }
  function stored(key) { try { return localStorage.getItem(key); } catch (e) { return null; } }

  function load(p) {
    var url = "/__agy/custom-plugins/file/" + encodeURIComponent(p.name) + "?v=" + encodeURIComponent(p.version);
    var el;
    if (p.kind === "css") {
      el = document.createElement("link");
      el.rel = "stylesheet";
      el.href = url;
    } else {
      el = document.createElement("script");
      el.src = url;
      // In the order they are listed.
      el.async = false;
    }
    el.setAttribute("data-agy-custom-plugin", p.name);
    el.onerror = function () { console.warn("[agy-custom-plugins] could not load " + p.name); };
    (document.head || document.documentElement).appendChild(el);
    window.__agyCustomPluginsLoaded.push(p.name);
    return el;
  }
  function unload(put) {
    if (put.el.parentNode) put.el.parentNode.removeChild(put.el);
    var loaded = window.__agyCustomPluginsLoaded, at = loaded.indexOf(put.name);
    if (at >= 0) loaded.splice(at, 1);
  }
  // A style's name and version, which say whether the one put in early is the one the list asks for.
  function key(p) { return p.name + "?" + p.version; }
  if (!safe) {
    var early = [], earlyKeys = [];
    try {
      JSON.parse(stored(STYLES_KEY) || "[]").forEach(function (p) {
        if (p && p.kind === "css" && typeof p.name === "string" && typeof p.version === "string") {
          early.push({ el: load(p), name: p.name });
          earlyKeys.push(key(p));
        }
      });
    } catch (e) {}
    fetch("/__agy/api/custom-plugins", { credentials: "same-origin", cache: "no-store" })
      .then(function (r) { return r.ok ? r.json() : null; })
      .then(function (d) {
        // Signed out or offline: the styles put in early stay, and so does what is kept for next time.
        if (!d || !d.plugins) return;
        var on = d.plugins.filter(function (p) { return p.enabled; });
        var styles = on.filter(function (p) { return p.kind === "css"; });
        store(STYLES_KEY, styles.length ? JSON.stringify(styles.map(function (p) { return { name: p.name, kind: p.kind, version: p.version }; })) : null);
        // The styles put in early stand when they are the list's, in its order; otherwise they are taken out and the
        // list's put in, which flashes only when something changed.
        var same = styles.map(key).join("\n") === earlyKeys.join("\n");
        if (!same) early.forEach(unload);
        on.forEach(function (p) { if (p.kind !== "css" || !same) load(p); });
      })
      .catch(function () {});
  }

  function api(path, opts) {
    return fetch(path, Object.assign({ credentials: "same-origin" }, opts || {})).then(function (r) {
      return r.json().catch(function () { return {}; }).then(function (d) {
        if (!r.ok) {
          var err = new Error(d.error || "HTTP " + r.status);
          err.notFile = !!d.notFile;
          throw err;
        }
        return d;
      });
    });
  }
  function postJSON(path, body) {
    return api(path, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
  }
  function size(n) {
    return n < 1024 ? n + " B" : n < 1048576 ? (n / 1024).toFixed(1) + " KB" : (n / 1048576).toFixed(1) + " MB";
  }
  function isPluginPath(p) { return /\.(js|css)$/i.test(p.trim()); }
  // The server's paths are in its own form: /home/me on Linux and macOS, C:\Users\me or \\host\share on Windows.
  function windowsPath(p) { return /^([A-Za-z]:|\\\\)/.test(p); }
  function sep(p) { return windowsPath(p) ? "\\" : "/"; }
  // The root of a path: "/", a drive such as "C:", or a share such as "\\host\share".
  function root(p) {
    if (!windowsPath(p)) return "";
    var m = /^([A-Za-z]:|\\\\[^\\/]+[\\/][^\\/]+)/.exec(p);
    return m ? m[1] : "";
  }
  function join(dir, name) {
    var s = sep(dir), base = dir.replace(windowsPath(dir) ? /[\\/]+$/ : /\/+$/, "");
    if (windowsPath(dir) && base.length < root(dir).length) base = root(dir);
    return base + s + name;
  }
  // The folders from the root down to dir, each with the path to open it.
  function crumbs(dir) {
    var s = sep(dir), top = root(dir), acc = top;
    var out = [{ label: top || s, path: top + s }];
    dir.slice(top.length).split(windowsPath(dir) ? /[\\/]/ : "/").filter(Boolean).forEach(function (part) {
      acc += s + part;
      out.push({ label: part, path: acc });
    });
    return out;
  }

  // "a", "a or b", "a, b or c".
  function listOf(items) {
    return items.length < 2 ? items.join("") : items.slice(0, -1).join(", ") + " or " + items[items.length - 1];
  }
  // Where a plugin is read from, as its row says it.
  function sourceLine(p) {
    if (p.sourceMissing) return (p.sourceProblem || "cannot read " + p.source) + ", so the saved copy is used";
    if (p.source) return p.source;
    if (p.copiedFrom) return "copied from " + p.copiedFrom;
    return "";
  }
  // Removing a linked or copied plugin deletes only the copy in the plugin folder; one with neither is that copy alone.
  function removeQuestion(p) {
    // A source that is there but cannot be used, such as one over 5 MB, is left as it is too.
    if (p.source && !p.sourceGone) return "Remove " + p.name + "? " + p.source + " is left as it is.";
    if (p.copiedFrom) return "Remove " + p.name + "? Its copy is deleted, and " + p.copiedFrom + " is left as it is.";
    return "Remove " + p.name + "? Its file in the plugin folder is the only copy, and it is deleted.";
  }

  // Material icon outlines (Apache 2.0), drawn as SVG as the app draws its own icons.
  var ICONS = {
    folder: "M10 4H4c-1.1 0-2 .9-2 2v12c0 1.1.9 2 2 2h16c1.1 0 2-.9 2-2V8c0-1.1-.9-2-2-2h-8l-2-2z",
    folder_open: "M20 6h-8l-2-2H4c-1.1 0-1.99.9-1.99 2L2 18c0 1.1.9 2 2 2h16c1.1 0 2-.9 2-2V8c0-1.1-.9-2-2-2zm0 12H4V8h16v10z",
    file: "M14 2H6c-1.1 0-2 .9-2 2v16c0 1.1.9 2 2 2h12c1.1 0 2-.9 2-2V8l-6-6zm4 18H6V4h7v5h5v11z",
    home: "M10 20v-6h4v6h5v-8h3L12 3 2 12h3v8z",
    arrow_upward: "M4 12l1.41 1.41L11 7.83V20h2V7.83l5.58 5.59L20 12l-8-8-8 8z",
  };
  function Icon(p) {
    var size = p.size || 16;
    return p.R.createElement("svg", { viewBox: "0 0 24 24", width: size, height: size, fill: "currentColor", "aria-hidden": "true",
      style: { flexShrink: 0, opacity: p.dim ? 0.7 : 1 } }, p.R.createElement("path", { d: ICONS[p.name] || ICONS.file }));
  }

  function Switch(p) {
    var h = p.R.createElement;
    return h("button", {
      type: "button", role: "switch", "aria-checked": p.on, "aria-label": p.label,
      "data-testid": p.testid || "agy-custom-plugin-switch", disabled: p.disabled, onClick: p.onClick,
      style: { width: 36, height: 20, borderRadius: 9999, border: "none", padding: 2, cursor: "pointer", flexShrink: 0,
        display: "inline-flex", alignItems: "center", background: p.on ? "var(--primary)" : "var(--secondary)",
        opacity: p.disabled ? 0.6 : 1, transition: "background 120ms" },
    }, h("span", { style: { width: 16, height: 16, borderRadius: 9999, transition: "transform 120ms",
      background: p.on ? "var(--primary-foreground)" : "var(--muted-foreground)",
      transform: p.on ? "translateX(16px)" : "translateX(0)" } }));
  }

  // The file chooser: a folder on the server, its path to edit or open by part, shortcuts, and its folders and plugin
  // files. Typing the path of a .js or .css file and pressing Enter adds it.
  function Chooser(p) {
    var R = p.R, h = R.createElement;
    var folderState = R.useState(null), folder = folderState[0], setFolder = folderState[1];
    var inputState = R.useState(""), input = inputState[0], setInput = inputState[1];
    var loadingState = R.useState(false), loading = loadingState[0], setLoading = loadingState[1];
    var hiddenState = R.useState(stored(HIDDEN_KEY) === "1"), showHidden = hiddenState[0], setShowHidden = hiddenState[1];

    function open(dir) {
      setLoading(true);
      p.onError("");
      api("/__agy/api/custom-plugins/browse?path=" + encodeURIComponent(dir || "")).then(function (d) {
        setFolder(d);
        setInput(d.path);
        store(LAST_DIR_KEY, d.path);
      }, function (e) {
        p.onError(e.message);
        // A folder kept from before that is gone opens the home folder instead.
        if (!folder) { store(LAST_DIR_KEY, null); if (dir) open(""); }
      }).then(function () { setLoading(false); });
    }
    R.useEffect(function () { open(stored(LAST_DIR_KEY) || ""); }, []);

    function go() {
      var v = input.trim();
      // Enter reaches here while the button is greyed out, and must not send the same request twice.
      if (!v || loading || p.busy) return;
      // A folder whose name ends in .js, such as highlight.js, is opened when it turns out not to be a file.
      if (isPluginPath(v)) p.onPick(v, function () { open(v); }); else open(v);
    }
    function toggleHidden() {
      var v = !showHidden;
      setShowHidden(v);
      store(HIDDEN_KEY, v ? "1" : null);
    }

    var btn = "px-2 py-1 rounded-md border border-border bg-transparent text-foreground text-xs cursor-pointer hover:bg-secondary inline-flex items-center gap-1";
    var entries = folder ? folder.entries.filter(function (e) { return showHidden || e.name.charAt(0) !== "."; }) : [];
    var hiddenCount = folder ? folder.entries.length - entries.length : 0;

    return h("div", { "data-testid": "agy-custom-plugin-browser", className: "flex flex-col" },
      h("div", { className: "py-2 px-3 w-full flex items-center gap-2" },
        h("input", { type: "text", value: input, spellCheck: false, autoCapitalize: "off", autoCorrect: "off",
          "aria-label": "Folder or file path on the server", "data-testid": "agy-custom-plugin-path",
          placeholder: "Folder, or a .js or .css file",
          className: "flex-1 min-w-0 bg-transparent border border-border rounded-md px-2 py-1 text-sm font-mono text-foreground",
          onChange: function (e) { setInput(e.target.value); },
          // Enter that ends composing a Korean or Japanese word is not a submit.
          onKeyDown: function (e) { if (e.key === "Enter" && !(e.nativeEvent && e.nativeEvent.isComposing) && e.keyCode !== 229) go(); } }),
        h("button", { type: "button", className: btn, disabled: loading || p.busy || !input.trim(), "data-testid": "agy-custom-plugin-go", onClick: go },
          isPluginPath(input) ? "Add" : "Open")),
      folder && h("div", { className: "px-3 pb-2 flex flex-wrap items-center gap-1 text-xs text-muted-foreground", "data-testid": "agy-custom-plugin-crumbs" },
        crumbs(folder.path).map(function (c, i, all) {
          var last = i === all.length - 1, s = sep(folder.path);
          return h(R.Fragment, { key: c.path },
            // "/" is a root and its own separator; a drive or share is followed by one.
            (s === "/" ? i > 1 : i > 0) && h("span", { "aria-hidden": "true" }, s),
            h("button", { type: "button", disabled: last || loading, "data-path": c.path,
              className: "bg-transparent border-none p-0 cursor-pointer font-mono " + (last ? "text-foreground" : "text-muted-foreground hover:text-foreground"),
              onClick: function () { open(c.path); } }, c.label));
        })),
      h("div", { className: "px-3 pb-2 flex flex-wrap items-center gap-2" },
        folder && folder.home && h("button", { type: "button", className: btn, disabled: loading, "data-testid": "agy-custom-plugin-home",
          onClick: function () { open(folder.home); } }, h(Icon, { R: R, name: "home", size: 14 }), "Home"),
        folder && folder.workspace && h("button", { type: "button", className: btn, disabled: loading, "data-testid": "agy-custom-plugin-workspace",
          onClick: function () { open(folder.workspace); } }, h(Icon, { R: R, name: "folder_open", size: 14 }), "Workspace"),
        folder && folder.parent && h("button", { type: "button", className: btn, disabled: loading, "data-testid": "agy-custom-plugin-up",
          onClick: function () { open(folder.parent); } }, h(Icon, { R: R, name: "arrow_upward", size: 14 }), "Up"),
        // The label and its switch stay together when the row wraps on a narrow screen.
        h("span", { className: "ml-auto inline-flex items-center gap-2" },
          h("span", { className: "text-xs text-muted-foreground" }, "Hidden"),
          h(Switch, { R: R, on: showHidden, label: showHidden ? "Hide hidden items" : "Show hidden items", testid: "agy-custom-plugin-hidden", onClick: toggleHidden }))),
      h("div", { style: { maxHeight: 300, overflowY: "auto" }, className: "border-t border-border" },
        !folder ? h("div", { className: "py-2 px-3 text-xs text-muted-foreground" }, loading ? "Loading…" : "") :
        entries.length === 0 ? h("div", { className: "py-2 px-3 text-xs text-muted-foreground" },
          hiddenCount ? "Only hidden items here. Turn on Hidden to see them." : "No folders or .js and .css files here.") :
        entries.map(function (e) {
          var unusable = !e.dir && !!e.unusable;
          return h("button", { key: (e.dir ? "d:" : "f:") + e.name, type: "button", disabled: loading || p.busy,
            "data-testid": e.dir ? "agy-custom-plugin-folder" : unusable ? "agy-custom-plugin-unusable" : "agy-custom-plugin-pick", "data-name": e.name,
            title: unusable ? e.unusable : undefined,
            className: "w-full text-left bg-transparent border-none cursor-pointer hover:bg-secondary py-1.5 px-3 flex items-center gap-2 text-sm " +
              (unusable ? "text-muted-foreground" : "text-foreground"),
            onClick: function () {
              if (e.dir) open(join(folder.path, e.name));
              else if (unusable) p.onError(e.name + ": " + e.unusable);
              else p.onPick(join(folder.path, e.name));
            } },
            h(Icon, { R: R, name: e.dir ? "folder" : "file", dim: e.dir || unusable }),
            h("span", { className: "flex-1 truncate min-w-0" }, e.name),
            !e.dir && h("span", { className: "text-xs text-muted-foreground" }, unusable ? "Cannot add" : size(e.size)));
        }),
        folder && folder.truncated && h("div", { className: "py-2 px-3 text-xs text-muted-foreground", "data-testid": "agy-custom-plugin-truncated" },
          "This folder has more items than are listed. Type a path to reach the others.")));
  }

  // The section, drawn with the page's own React as a component so it can keep state.
  window.__agyCustomPluginsSection = function AgyCustomPluginsSection(props) {
    var R = props.React, h = R.createElement;
    var listState = R.useState(null), list = listState[0], setList = listState[1];
    var errorState = R.useState(""), error = errorState[0], setError = errorState[1];
    var busyState = R.useState(false), busy = busyState[0], setBusy = busyState[1];
    var changedState = R.useState(false), changed = changedState[0], setChanged = changedState[1];
    var choosingState = R.useState(false), choosing = choosingState[0], setChoosing = choosingState[1];
    var copiedState = R.useState([]), copiedFolders = copiedState[0], setCopiedFolders = copiedState[1];
    var stateProblemState = R.useState(""), stateProblem = stateProblemState[0], setStateProblem = stateProblemState[1];
    function show(d) { setList(d.plugins || []); setCopiedFolders(d.copiedFolders || []); setStateProblem(d.stateProblem || ""); }

    R.useEffect(function () {
      var live = true;
      api("/__agy/api/custom-plugins").then(function (d) { if (live) show(d); },
        function (e) { if (live) { setList([]); setError(e.message); } });
      return function () { live = false; };
    }, []);

    function run(p, then, fail) {
      setBusy(true);
      setError("");
      p.then(function (d) { show(d); setChanged(true); if (then) then(); },
        function (e) { if (!(fail && fail(e))) setError(e.message); })
        .then(function () { setBusy(false); });
    }
    function pick(path, asFolder) {
      run(postJSON("/__agy/api/custom-plugins/add-path", { path: path }), function () { setChoosing(false); },
        function (e) { if (e.notFile && asFolder) { asFolder(); return true; } return false; });
    }
    function toggle(p) { run(postJSON("/__agy/api/custom-plugins/toggle", { name: p.name, enabled: !p.enabled })); }
    function remove(p) {
      if (window.confirm(removeQuestion(p))) run(postJSON("/__agy/api/custom-plugins/delete", { name: p.name }));
    }
    function reload() {
      if (safe) {
        try { sessionStorage.removeItem(SAFE_KEY); } catch (e) {}
        var u = new URL(location.href);
        u.searchParams.delete("agy-safe");
        location.replace(u.toString());
        return;
      }
      location.reload();
    }

    var btn = "px-2 py-1 rounded-md border border-border bg-transparent text-foreground text-xs cursor-pointer hover:bg-secondary";
    var row = "py-2 px-3 w-full bg-transparent flex justify-between gap-4 items-center";
    var note = function (text) { return h("div", { className: row }, h("span", { className: "text-xs text-muted-foreground" }, text)); };

    return h("div", { className: "space-y-2", "data-testid": "agy-custom-plugins-section" },
      h("div", { className: "flex flex-col gap-1" },
        h("h3", { className: "m-0 font-medium" }, "Custom Plugins"),
        h("p", { className: "m-0 text-xs text-muted-foreground" },
          "Load your own JavaScript or CSS files from this server into the web app. These are not Antigravity's agent plugins or its UI Plugins under Customizations. A plugin runs with the same access as the app, including the agent and its terminal, so add only files you trust."),
        h("p", { className: "m-0 text-xs text-muted-foreground" },
          "Extensions made for the desktop app that need its helper process or Chrome debugging, such as account switching, usage dashboards or proxies, do not work here.")),
      stateProblem && h("div", { className: "text-xs text-red-500", role: "alert", "data-testid": "agy-custom-plugins-state-problem" }, stateProblem),
      h("div", { className: "agy-card bg-transparent rounded-xl border border-border overflow-hidden divide-y divide-border" },
        list === null ? note("Loading…") : list.length === 0 ? note("No plugins yet.") : list.map(function (p) {
          var where = sourceLine(p);
          return h("div", { key: p.name, className: row, "data-testid": "agy-custom-plugin-row", "data-plugin": p.name },
            h("div", { className: "flex-1 flex flex-col gap-0.5 min-w-0" },
              h("span", { className: "truncate font-mono text-sm" }, p.name),
              h("span", { className: "text-xs text-muted-foreground" }, (p.kind === "css" ? "CSS" : "JavaScript") + " · " + size(p.size)),
              where && h("span", { className: "truncate font-mono text-xs " + (p.sourceMissing ? "text-red-500" : "text-muted-foreground"),
                title: p.source || p.copiedFrom, "data-testid": "agy-custom-plugin-source" }, where),
              p.copiedFrom && h("span", { className: "text-xs text-muted-foreground", "data-testid": "agy-custom-plugin-copied" },
                "Copied, not linked, since the agent can change files there. Add it again to take in edits.")),
            h("div", { className: "flex items-center gap-2" },
              h(Switch, { R: R, on: p.enabled, disabled: busy, label: (p.enabled ? "Turn off " : "Turn on ") + p.name, onClick: function () { toggle(p); } }),
              h("button", { type: "button", className: btn, disabled: busy, onClick: function () { remove(p); } }, "Remove")));
        }),
        h("div", { className: row },
          h("div", { className: "flex-1 flex flex-col gap-0.5" },
            h("span", null, "Add a file"),
            h("span", { className: "text-xs text-muted-foreground" },
              "A .js or .css file on the server, up to 5 MB. Edits to it apply on the next reload." +
              (copiedFolders.length ? " A file the agent can change, in " + listOf(copiedFolders) + ", is copied instead, and does not take in edits." : ""))),
          h("button", { type: "button", className: btn, disabled: busy, "data-testid": "agy-custom-plugin-choose",
            onClick: function () { setChoosing(!choosing); } }, choosing ? "Close" : "Choose file")),
        choosing && h(Chooser, { R: R, busy: busy, onPick: pick, onError: setError })),
      safe ? h("div", { className: "flex items-center justify-between gap-2" },
        h("span", { className: "text-xs text-muted-foreground" }, "Safe mode: plugins are not loaded in this tab."),
        h("button", { type: "button", className: btn, onClick: reload }, "Leave safe mode"))
        : changed && h("div", { className: "flex items-center justify-between gap-2" },
          h("span", { className: "text-xs text-muted-foreground" }, "Reload to apply the changes."),
          h("button", { type: "button", className: btn, "data-testid": "agy-custom-plugins-reload", onClick: reload }, "Reload")),
      error && h("div", { className: "text-xs text-red-500", role: "alert" }, error));
  };
})();
