// fell site: an interactive replica of the TUI, plus small page helpers.
// Plain JS, no dependencies, no network. The worktrees come from sample.js,
// which site/build.sh generates from fell's own read-only output
// (`fell scan --json`, `fell capabilities --json`) on the dummy workspace
// demo/setup.sh builds: generic data, never a real machine. The layout
// mirrors internal/ui/view.go; actions are a dry run decided from the
// scan's own `actions[]`, exactly as the TUI's confirmation does.
(() => {
  "use strict";
  document.documentElement.classList.add("js");
  const reduceMotion = matchMedia("(prefers-reduced-motion: reduce)").matches;
  const $ = (s, el = document) => el.querySelector(s);
  const $$ = (s, el = document) => [...el.querySelectorAll(s)];

  /* ---------- copy buttons ---------- */
  const copy = async (text) => {
    try { await navigator.clipboard.writeText(text); }
    catch {
      const t = Object.assign(document.createElement("textarea"), { value: text });
      document.body.append(t); t.select(); document.execCommand("copy"); t.remove();
    }
  };
  $$("[data-copy]").forEach((b) => b.addEventListener("click", async () => {
    await copy(b.dataset.copy);
    b.classList.add("done");
    clearTimeout(b._t); b._t = setTimeout(() => b.classList.remove("done"), 1600);
  }));

  /* ---------- install tabs (ARIA tabs pattern) ---------- */
  const tabs = $$('[role="tab"]');
  const pick = (t) => {
    tabs.forEach((x) => {
      const on = x === t;
      x.setAttribute("aria-selected", on); x.tabIndex = on ? 0 : -1;
      $("#" + x.getAttribute("aria-controls")).hidden = !on;
    });
    t.focus();
  };
  tabs.forEach((t, i) => {
    t.addEventListener("click", () => pick(t));
    t.addEventListener("keydown", (e) => {
      const d = e.key === "ArrowRight" ? 1 : e.key === "ArrowLeft" ? -1 : 0;
      if (d) { e.preventDefault(); pick(tabs[(i + d + tabs.length) % tabs.length]); }
    });
  });

  /* ---------- reveal on scroll ---------- */
  if (!reduceMotion && "IntersectionObserver" in window) {
    const io = new IntersectionObserver((es) => es.forEach((e) => {
      if (e.isIntersecting) { e.target.classList.add("in"); io.unobserve(e.target); }
    }), { rootMargin: "0px 0px -8% 0px" });
    $$(".sec h2, .sec .intro, .leak, .own, .cut, .safety article, .checks li, .levels, #evidence .window, .cmp-wrap, .recording, .platforms div, .usage, .codes").forEach((el, i) => {
      el.classList.add("reveal"); el.style.transitionDelay = (i % 4) * 60 + "ms"; io.observe(el);
    });
  }

  /* ---------- recording: video, GIF fallback ---------- */
  const video = $("#demo-video"), play = $("#play");
  if (video) {
    const toGif = () => {
      const img = Object.assign(new Image(1280, 580), { src: "demo.gif", alt: video.getAttribute("aria-label"), loading: "lazy" });
      video.replaceWith(img); play.hidden = true;
    };
    const srcs = $$("source", video);
    srcs[srcs.length - 1].addEventListener("error", toGif);
    const start = (manual) => { play.hidden = true; if (manual === true) video.controls = true; video.play().catch(() => { play.hidden = false; }); };
    play.addEventListener("click", () => start(true));
    video.addEventListener("click", () => { if (!video.controls) { if (video.paused) video.play(); else video.pause(); } });
    const saveData = navigator.connection && navigator.connection.saveData;
    if (!reduceMotion && !saveData && "IntersectionObserver" in window) {
      const vio = new IntersectionObserver((es) => es.forEach((e) => {
        if (e.isIntersecting) start(); else if (!video.paused) video.pause();
      }), { threshold: 0.5 });
      vio.observe(video);
    }
  }

  /* =====================================================================
     The TUI replica (internal/ui)
     ===================================================================== */
  const term = $("#term");
  const DATA = window.FELL_SAMPLE;
  if (!term) return;
  if (!DATA) { term.innerHTML = '<img src="demo.gif" alt="fell scanning a dummy workspace" style="width:100%;height:auto">'; return; }
  const keysEl = $("#term-keys"), hint = $("#try-hint");
  const SCAN = DATA.scan, SHORT = DATA.short || {};
  const TRASH = (DATA.caps && DATA.caps.trash) || "Trash";
  const WHERE = SCAN.roots.join(", ") + " + agent locations";
  const BAR = 14, DETAIL = 6, CHROME = 6 + DETAIL;

  /* ---------- formatting (internal/format) ---------- */
  const bytes = (n) => {
    if (n < 0) return "…";
    if (n < 1000) return n + " B";
    let div = 1000, exp = 0;
    for (let m = Math.floor(n / 1000); m >= 1000; m = Math.floor(m / 1000)) { div *= 1000; exp++; }
    return (n / div).toFixed(1) + " " + "kMGTPE"[exp] + "B";
  };
  const ago = (it) => {
    const a = it.activity;
    if (!a || !a.last) return "?";
    const d = a.age_days;
    return d < 1 ? "today" : d < 14 ? d + "d ago" : d < 60 ? Math.floor(d / 7) + "w ago" : d < 365 ? Math.floor(d / 30) + "mo ago" : (d / 365).toFixed(1) + "y ago";
  };
  const padR = (s, n) => (s.length >= n ? s : s + " ".repeat(n - s.length));
  const padL = (s, n) => (s.length >= n ? s : " ".repeat(n - s.length) + s);
  const esc = (s) => String(s).replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]));
  const base = (p) => p.replace(/\/+$/, "").split("/").pop();
  const dir = (p) => p.replace(/\/+$/, "").split("/").slice(0, -1).join("/");

  /* ---------- the scanned worktrees (contract items) ---------- */
  const total = (it) => (it.bytes ? it.bytes.allocated : 0);
  const strip = (it) => (it.strippable_bytes ? it.strippable_bytes.allocated : 0);
  const name = (it) => { let b = base(it.path); if (b === it.project.name) b = base(dir(it.path)); return it.project.name + "/" + b; };
  const dirty = (it) => { const d = it.git.dirty; return d.staged + d.modified + d.untracked + d.conflicts; };
  const atRisk = (it) => it.precious.filter((p) => p.at_risk).length;
  const decide = (it, action) => it.actions.find((a) => a.action === action) || { allowed: false, blocked: ["unknown_state"] };
  const possible = (d) => !(d.blocked && d.blocked.length);
  const permits = (d, force, unlock) => possible(d) && (!(d.needs_override || []).length || force) && (!d.needs_unlock || unlock);
  const LV = { safe: "g", caution: "y", protected: "r", live: "m" };
  const ICON = { safe: "●", caution: "▲", protected: "■", live: "◉" };
  const flags = (it) => {
    const f = [];
    const g = it.git;
    if (it.kind === "prunable") f.push("checkout missing");
    else if (it.kind === "orphan") f.push("orphaned");
    else if (g.detached) f.push("detached");
    else f.push(g.branch);
    if (g.unpushed > 0) f.push("↑" + g.unpushed);
    if (g.only_here > 0) f.push("only-here:" + g.only_here);
    if (dirty(it) > 0) f.push("✎" + dirty(it));
    if (atRisk(it) > 0) f.push("secrets:" + atRisk(it));
    if (g.locked) f.push("locked");
    if (it.liveness.in_use) f.push("in-use");
    if (it.owner.tracked) f.push("app-tracked");
    return f;
  };
  const flagClass = (f) => (/^(↑|only-here|✎)/.test(f) || f === "in-use" ? "y" : /^secrets/.test(f) || ["locked", "orphaned", "checkout missing"].includes(f) ? "r" : f === "app-tracked" ? "m" : "f");

  /* ---------- state (internal/ui/model.go) ---------- */
  const fresh = () => ({ view: "scanning", group: 0, sort: 0, cursorKey: "", offset: 0, selected: new Set(), status: {}, outcomes: [], queue: [], confirm: null, shell: [] });
  let st = fresh(), cols = 110, lines = 26, timer = null;
  const SORTS = ["size", "oldest", "name"];
  const items = () => SCAN.items;

  const rows = () => {
    const groups = new Map(), title = {};
    items().forEach((it) => {
      let g = it.owner.id, t = it.owner.name;
      if (st.group === 1) { g = it.project.root + "\0" + it.project.name; t = it.project.name; if (!it.project.root) { g = "orphans"; t = "orphaned checkouts"; } }
      if (!groups.has(g)) { groups.set(g, []); title[g] = t; }
      groups.get(g).push(it);
    });
    let order = [...groups.keys()];
    if (st.group === 1) order.sort((a, b) => title[a].toLowerCase().localeCompare(title[b].toLowerCase()));
    const out = [];
    order.forEach((g) => {
      const ws = groups.get(g).slice();
      const t0 = (it) => (it.activity && it.activity.last ? Date.parse(it.activity.last) : 0);
      ws.sort((a, b) => (st.sort === 1 ? t0(a) - t0(b) : st.sort === 2 ? (name(a) < name(b) ? -1 : name(a) > name(b) ? 1 : 0) : total(b) - total(a)));
      out.push({ group: g, title: title[g], w: null, key: "group:" + g });
      ws.forEach((w) => out.push({ group: g, title: title[g], w, key: w.id }));
    });
    return out;
  };
  const cursor = (rs) => Math.max(0, rs.findIndex((r) => r.key === st.cursorKey));
  const leaves = (r) => (r.w ? [r.w.id] : rows().filter((x) => x.w && x.group === r.group).map((x) => x.w.id));
  const find = (id) => items().find((it) => it.id === id);
  const bodyHeight = () => Math.max(3, lines - CHROME);
  const scroll = () => {
    const rs = rows(), cur = cursor(rs), body = bodyHeight() - 1;
    if (cur < st.offset) st.offset = cur;
    if (cur >= st.offset + body) st.offset = cur - body + 1;
    st.offset = Math.max(0, Math.min(st.offset, rs.length - body));
  };
  const busy = () => st.queue.length > 0 || Object.values(st.status).includes("working");

  /* ---------- lines: segments truncated to the terminal width ---------- */
  // A line is a list of [class, text] segments, or {html, w} for fixed-width pieces.
  const S = (cls, text) => [cls, String(text)];
  const render1 = (segs, extra = "") => {
    let room = cols, html = "", cut = false;
    for (const s of segs) {
      if (cut) break;
      if (!Array.isArray(s)) { if (s.w <= room) { html += s.html; room -= s.w; } else cut = true; continue; }
      const [cls, text] = s;
      let t = text;
      if ([...t].length > room) { t = [...t].slice(0, Math.max(0, room - 1)).join("") + "…"; cut = true; }
      room -= [...t].length;
      html += cls ? `<span class="${cls}">${esc(t)}</span>` : esc(t);
    }
    return html || " ";
  };
  const ln = (segs, cls = "", attrs = "") => `<span class="ln${cls ? " " + cls : ""}"${attrs}>${render1(segs)}</span>`;
  const blank = () => ln([]);
  const sep = S("f", "  ·  ");

  const measure = () => {
    const probe = document.createElement("span");
    probe.textContent = "0".repeat(100);
    probe.style.cssText = "position:absolute;visibility:hidden;white-space:pre";
    term.append(probe);
    const cw = probe.getBoundingClientRect().width / 100;
    probe.remove();
    const cs = getComputedStyle(term);
    const lh = parseFloat(cs.lineHeight) || 18;
    cols = Math.max(30, Math.floor((term.clientWidth - parseFloat(cs.paddingLeft) - parseFloat(cs.paddingRight)) / cw));
    lines = Math.max(20, Math.round((term.clientHeight - parseFloat(cs.paddingTop) - parseFloat(cs.paddingBottom)) / lh));
  };

  /* ---------- header ---------- */
  const header = () => {
    const segs = [{ html: '<span class="brandb">fell</span>', w: 6 }, S("", " "), S("y", "● dry run")];
    if (st.view !== "scanning") {
      let t = 0, s = 0, sh = 0, n = 0;
      items().forEach((it) => { if (st.status[it.id] === "done") return; n++; t += total(it); s += strip(it); sh += it.strippable_bytes ? it.strippable_bytes.shared : 0; });
      segs.push(sep, S("b", bytes(t)), S("", ` in ${n} worktrees`), sep, S("c", bytes(s)), S("", " rebuildable" + (sh > 0 ? ` (${bytes(s - sh)} freeable)` : "")));
    }
    segs.push(sep, S("f", WHERE));
    return ln([S("", " "), ...segs]);
  };

  /* ---------- list rows ---------- */
  const check = (ids) => {
    let sel = 0, fin = 0;
    for (const id of ids) {
      const s = st.status[id];
      if (s === "done") fin++;
      else if (s === "working" || s === "queued") return S("f", "◌");
      else if (st.selected.has(id)) sel++;
    }
    if (ids.length && fin === ids.length) return S("g", "✓");
    if (sel > 0 && sel + fin === ids.length) return S("c", "●");
    if (sel > 0) return S("c", "◐");
    return S("f", "○");
  };
  const LVCOL = { g: "var(--safe)", y: "var(--review)", r: "var(--protected)", m: "var(--live)" };
  const bar = (it, largest) => {
    const t = total(it);
    if (!it.measured || t <= 0) return { html: `<span class="f">${"·".repeat(BAR)}</span>`, w: BAR };
    const units = Math.max(1, Math.min(Math.floor((t * BAR * 8) / largest), BAR * 8));
    const cells = Math.ceil(units / 8), filled = units / 8;
    const reb = Math.floor((cells * strip(it)) / t); // cells drawn cyan, as in view.go
    const cyan = reb >= cells ? filled : reb;
    return {
      html: `<span class="bar"><i style="width:${cyan}ch;background:var(--cyan)"></i><i style="width:${filled - cyan}ch;background:${LVCOL[LV[it.safety.level]]}"></i>` +
        `<i style="width:${cells - filled}ch"></i><u>${"·".repeat(BAR - cells)}</u></span>`, w: BAR,
      text: "█".repeat(Math.floor(units / 8)) + ["", "▏", "▎", "▍", "▌", "▋", "▊", "▉"][units % 8] + "·".repeat(BAR - cells),
    };
  };
  const row = (r, isCur, largest, i) => {
    const cur = isCur ? S("g", "▌") : S("", " ");
    if (!r.w) {
      const ws = rows().filter((x) => x.w && x.group === r.group);
      const size = ws.reduce((a, x) => a + total(x.w), 0);
      return ln([cur, S("", " "), check(leaves(r)), S("", " "), S("hd", r.title), S("", "  "), S("f", `${ws.length} worktree(s) · ${bytes(size)}`)], "row" + (isCur ? " on" : ""), ` data-row="${i}"`);
    }
    const w = r.w;
    let share = "    ";
    if (w.kind === "worktree" && total(w) > 0) share = padL(Math.floor((strip(w) * 100) / total(w)) + "%", 4);
    const d = w.activity && w.activity.last ? w.activity.age_days : -1;
    const ageCls = d < 0 ? "f" : d < 7 ? "g" : d >= 90 ? "y" : "";
    const lvl = w.safety.level;
    const status = st.status[w.id];
    let segs = [bar(w, largest), S("", "  " + padL(bytes(total(w)), 9) + "  "), S("c", share), S("", "  "), S(ageCls, padR(ago(w), 8)), S("", "  "),
      S(LV[lvl], ICON[lvl] + " " + padR(lvl, 9)), S("", " "), S("b", name(w)), S("", "  ")];
    flags(w).forEach((f, j) => { if (j) segs.push(S("", " ")); segs.push(S(flagClass(f), f)); });
    const plain = () => segs.map((s) => (Array.isArray(s) ? s[1] : s.text || "·".repeat(BAR))).join("");
    if (status === "done") segs = [S("s", plain())];
    else if (status === "working" || status === "queued") segs = [S("f", plain())];
    return ln([cur, S("", " "), check([w.id]), S("", " "), ...segs], "row" + (isCur ? " on" : "") + (status === "done" ? " done" : ""), ` data-row="${i}"`);
  };

  /* ---------- detail pane ---------- */
  const decision = (name, d) => {
    if (d.allowed) return S("g", name + " ✓");
    if (!possible(d)) return S("r", name + " ✗ " + d.blocked.join(","));
    const need = [];
    if ((d.needs_override || []).length) need.push("force: " + d.needs_override.join(","));
    if (d.needs_unlock) need.push("unlock");
    return S("y", name + " needs " + need.join("; "));
  };
  const wordwrap = (s, width) => {
    const out = []; let cur = "";
    s.split(" ").forEach((w) => { if (cur && (cur + " " + w).length > width) { out.push(cur); cur = w; } else cur = cur ? cur + " " + w : w; });
    if (cur) out.push(cur);
    return out;
  };
  const detail = (r) => {
    const pad = S("", "  ");
    if (!r.w) return [[pad, S("hd", r.title)], [pad, S("f", "space selects the whole group · s strip · r retire")]];
    const w = r.w, out = [];
    let where = w.owner.name;
    if (w.git.detached) where += " · detached at " + w.git.head.slice(0, 8);
    else if (w.git.branch) where += " · branch " + w.git.branch;
    const src = { commit: "last commit", git: "last git activity", files: "last file change" }[(w.activity || {}).source] || "activity unknown";
    out.push([pad, S("b", name(w)), S("", "  "), S("f", `${where} · ${src} ${ago(w)}`)]);
    let where2 = `${w.path}  [${w.id}]`;
    if (w.measured && w.kind !== "prunable") where2 += `  ·  ${bytes(total(w))} on disk` + (w.bytes.shared > 0 ? ` (${bytes(w.bytes.exclusive)} freeable)` : "");
    out.push([pad, S("f", where2)]);
    const ev = [];
    if (w.artifacts.length) {
      const arts = w.artifacts.map((a) => a.path + (w.measured ? " " + bytes(a.bytes.allocated) + (a.bytes.shared > 0 ? ` (${bytes(a.bytes.shared)} shared)` : "") : "") + (a.eligible ? "" : " (kept)"));
      ev.push([S("c", "rebuildable: "), S("", arts.join(", "))]);
    }
    let commits = S("", "commits: all on a remote");
    if (w.kind === "orphan") commits = S("", "commits: unknown (git record gone)");
    else if (w.git.only_here > 0) commits = S("y", `commits: ${w.git.only_here} only here (bundled on retire)`);
    else if (w.git.unpushed > 0) commits = S("y", `commits: ${w.git.unpushed} not pushed, kept on branch ${w.git.branch}`);
    else if (!w.git.has_remote) commits = S("", "commits: repo has no remote; all on local branches");
    ev.push([commits]);
    if (dirty(w) > 0) ev.push([S("y", "uncommitted: " + dirty(w))]);
    const prec = w.precious.filter((p) => p.at_risk || p.symlink).map((p) => p.path + (p.at_risk ? "" : "→link"));
    if (prec.length) ev.push([S("r", "ignored secrets/data: "), S("", prec.join(", "))]);
    const evl = [pad];
    ev.forEach((e, i) => { if (i) evl.push(S("f", "  ·  ")); evl.push(...e); });
    out.push(evl);
    out.push([pad, S(LV[w.safety.level], w.safety.level), S("", "  "), decision("strip", decide(w, "strip")), S("", "  "), decision("retire", decide(w, "retire"))]);
    const why = w.safety.reasons.filter((x) => x.code !== "app_tracked" && x.code !== "ignored_precious").map((x) => x.detail);
    if (why.length) out.push([pad, S("f", why.join(" · "))]);
    if (w.owner.tracked) wordwrap(`${w.owner.name} tracks it. ${w.owner.how_to_remove}`, Math.max(20, cols - 4)).forEach((l) => out.push([pad, S("m", l)]));
    return out.slice(0, DETAIL);
  };

  /* ---------- summary and key hints ---------- */
  const summary = () => {
    let sel = 0, b = 0;
    st.selected.forEach((id) => { const w = find(id); if (w) { sel++; b += total(w); } });
    const parts = [S("", `${sel} selected (${bytes(b)})`)];
    const ok = st.outcomes.filter((o) => o.status === "dry_run");
    if (ok.length) parts.push(sep, S("g", "Would move: " + bytes(ok.reduce((a, o) => a + o.estimate, 0))));
    if (st.queue.length) parts.push(sep, S("f", `${st.queue.length} queued`));
    return ln([S("", "  "), ...parts]);
  };
  const help = () => {
    if (st.view === "legend") return ln([S("f", "  any key closes")]);
    if (st.confirm) return ln([S("y", "  Proceed?"), S("", "  "), S("b", "y"), S("f", " yes   "), S("b", "f"), S("f", " toggle force   "), S("b", "u"), S("f", " toggle unlock   any other key cancels")]);
    return ln([S("f", "  ↑↓ move  space select  a all  s strip  r retire  tab group  o sort  R rescan  ? legend  q quit")]);
  };

  /* ---------- confirmation (confirmLines) ---------- */
  const plan = () => {
    const run = [], refuse = [];
    st.confirm.ids.forEach((id) => { const w = find(id); if (!w) return; (permits(decide(w, st.confirm.action), st.confirm.force, st.confirm.unlock) ? run : refuse).push(w); });
    return { run, refuse };
  };
  const estimate = (action, w) => (action === "strip" ? w.artifacts.filter((a) => a.eligible).reduce((s, a) => s + a.bytes.allocated, 0) : total(w));
  const saves = (action, w) => {
    if (action === "strip") return S("c", w.artifacts.filter((a) => a.eligible).map((a) => a.path).join(", "));
    const s = [];
    if (w.git.only_here > 0) s.push(`bundle ${w.git.only_here} commit(s)`);
    if (dirty(w) > 0) s.push("uncommitted patch");
    if (atRisk(w) > 0) s.push(`${atRisk(w)} secret file(s)`);
    return s.length ? S("y", "saves " + s.join(", ")) : S("f", "nothing extra to save");
  };
  const blockedText = (w, action, codes) => codes.map((c) => {
    if (c === "unknown_state" && action === "strip" && w.kind !== "worktree") return "no checkout to strip";
    const r = w.safety.reasons.find((x) => x.code === c);
    return r ? r.detail : c;
  }).join("; ");
  const confirmLines = (height) => {
    const c = st.confirm, { run, refuse } = plan();
    const sum = run.reduce((a, w) => a + (c.action === "strip" ? strip(w) : total(w)), 0);
    const verb = c.action === "strip" ? "Dry run: strip rebuildable artifacts from" : "Dry run: retire";
    const toggles = [];
    if (c.force) toggles.push(S("r", "  [force]"));
    if (c.unlock) toggles.push(S("r", "  [unlock]"));
    const head = run.length ? S("y", `  ${verb} ${run.length} worktree(s), about ${bytes(sum)} (nothing will change)`) : S("y", `  Nothing to ${c.action} yet: every worktree below is held back.`);
    const out = [[], [head, ...toggles], []];
    if (c.action === "retire") out.push([S("f", `  Each: rescue folder (commits only it has, uncommitted patch, ignored secrets) → ${TRASH},`)], [S("f", `        checkout → ${TRASH}, then \`git worktree prune\` if only its own record would go. Branches are kept.`)], []);
    else out.push([S("f", "  Only git-ignored, untracked directories from the catalog move; the worktree stays.")], []);
    const room = height - out.length - 2;
    for (let i = 0; i < run.length; i++) {
      const w = run[i];
      if (i >= Math.floor(room / 2) && run.length > Math.floor(room / 2)) { out.push([S("f", `    … and ${run.length - i} more`)]); break; }
      out.push([S("", "    " + padL(bytes(total(w)), 9) + "  "), S("b", name(w)), S("", "  "), saves(c.action, w)]);
    }
    if (refuse.length) {
      out.push([], [S("r", `  Not included (${refuse.length}):`)]);
      for (let i = 0; i < refuse.length; i++) {
        if (out.length >= height - 1) { out.push([S("f", `    … and ${refuse.length - i} more`)]); break; }
        const w = refuse[i], d = decide(w, c.action);
        let why = blockedText(w, c.action, d.blocked || []);
        if (!why) {
          const need = [];
          if ((d.needs_override || []).length && !c.force) need.push(`press f to force (${d.needs_override.join(", ")})`);
          if (d.needs_unlock && !c.unlock) need.push("press u to unlock");
          why = need.join("; ");
        } else if (w.owner.tracked && c.action === "retire") {
          why = `tracked by ${w.owner.name}; remove it the official way: ${SHORT[w.owner.id] || w.owner.how_to_remove}`;
        }
        out.push([S("", "    " + name(w) + "  "), S("f", why)]);
      }
    }
    return out;
  };

  const legendLines = () => {
    const badge = (l) => S(LV[l], ICON[l] + " " + padR(l, 9));
    return [[],
      [S("", "  "), S("b", "Safety")],
      [S("", "    "), badge("safe"), S("", "  nothing would be lost; retire allowed")],
      [S("", "    "), badge("caution"), S("", "  allowed; something is saved to the rescue folder or worth a look")],
      [S("", "    "), badge("protected"), S("", "  retire needs force/unlock, or is blocked (app-tracked, keep marker)")],
      [S("", "    "), badge("live"), S("", "  its app or session is running; fell will not touch it")],
      [],
      [S("", "  "), S("b", "Bar"), S("", "  "), S("c", "███"), S("", " rebuildable artifacts  "), S("g", "██▍"), S("", " the rest, in the safety colour; length is relative to the largest")],
      [],
      [S("", "  "), S("b", "strip"), S("", `   moves node_modules, .venv, build output … (git-ignored, nothing tracked) to the ${TRASH}; keeps the worktree`)],
      [S("", "  "), S("b", "retire"), S("", `  saves a rescue folder, moves the checkout to the ${TRASH}, prunes git's record; keeps the branch`)],
      [],
      [S("", '  Sizes count hard links once; on macOS, blocks shared with APFS clones or snapshots are "shared" and not counted as freeable.')],
      [S("", "  Freed space is measured from the disk after acting, not assumed.")],
      [S("", "  Worktrees an app still tracks are only stripped; fell shows how that app removes them.")],
    ];
  };

  /* ---------- after q: the shell, with what fell printed ---------- */
  const stamp = () => { const d = new Date(), p = (n) => String(n).padStart(2, "0"); return `${d.getFullYear()}${p(d.getMonth() + 1)}${p(d.getDate())}-${p(d.getHours())}${p(d.getMinutes())}${p(d.getSeconds())}`; };
  const clean = (s) => s.replace(/[^A-Za-z0-9._-]+/g, "-").replace(/^-+|-+$/g, "");
  const receipt = () => {
    const out = [[S("f", "$ "), S("", "fell --dry-run")]];
    let est = 0;
    st.outcomes.forEach((o) => {
      est += o.estimate;
      out.push([S("", padR("would " + o.action, 14) + " " + padL(bytes(o.estimate), 9) + "  " + o.w.path)]);
      if (o.action === "retire") {
        const w = o.w, parts = [];
        if (w.git.only_here > 0) parts.push(`${w.git.only_here} commit(s) bundled`);
        if (w.git.dirty.staged + w.git.dirty.modified + w.git.dirty.conflicts > 0) parts.push("uncommitted changes as a patch");
        if (atRisk(w) > 0) parts.push(`${atRisk(w)} secret file(s) copied`);
        out.push([S("", `               rescue folder fell-rescue-${clean(w.project.name)}-${clean(base(w.path))}-${o.stamp}: ${parts.join(", ") || "RESTORE.md only"}`)]);
      }
    });
    if (st.outcomes.length) out.push([S("", `Dry run: ${st.outcomes.length} action(s) would move about ${bytes(est)}. Nothing was changed.`)]);
    return out;
  };
  const JQ = "fell scan --json | jq -c '.items[] | [.id, .name, .safety.level]'";
  const shellLines = () => {
    const out = st.shell.slice();
    out.push([S("f", "$ "), { html: '<span class="caret"> </span>', w: 1 }]);
    return out;
  };

  /* ---------- render ---------- */
  const render = () => {
    measure();
    let html = "";
    if (st.view === "shell") {
      const ls = shellLines();
      html = ls.slice(Math.max(0, ls.length - lines)).map((l) => ln(l)).join("");
      term.innerHTML = `<div class="screen">${html}</div>`;
      return renderKeys();
    }
    scroll();
    html += header() + blank();
    const rs = rows(), cur = cursor(rs);
    let body = bodyHeight();
    let ls = [];
    if (st.view === "scanning") ls = [[S("scanning", "  Scanning " + WHERE + " …")]];
    else if (st.view === "legend") ls = legendLines();
    else if (st.confirm) ls = confirmLines(body + 1);
    else {
      html += ln([S("f", "    " + padR("", BAR) + "  " + padL("SIZE", 9) + "  " + padL("REB.", 4) + "  " + padR("ACTIVE", 8) + "  " + padR("SAFETY", 10) + " WORKTREE  (sort: " + SORTS[st.sort] + ")")]);
      body--;
      const largest = Math.max(1, ...items().map(total));
      for (let i = st.offset; i < Math.min(rs.length, st.offset + body); i++) ls.push({ row: rs[i], i });
    }
    for (let i = 0; i < body; i++) {
      const l = ls[i];
      if (!l) html += blank();
      else if (l.row) html += row(l.row, l.i === cur, Math.max(1, ...items().map(total)), l.i);
      else html += ln(l);
    }
    html += ln([S("f", "─".repeat(cols))]);
    const det = st.view === "list" && !st.confirm && rs.length ? detail(rs[cur]) : [];
    for (let i = 0; i < DETAIL; i++) html += det[i] ? ln(det[i]) : blank();
    html += st.view === "scanning" ? blank() : summary();
    html += help();
    term.innerHTML = `<div class="screen">${html}</div>`;
    renderKeys();
  };

  const KEYSETS = {
    list: [["space", "select"], ["a", "all"], ["s", "strip"], ["r", "retire", true], ["tab", "group"], ["o", "sort"], ["R", "rescan"], ["?", "legend"], ["q", "quit"]],
    confirm: [["y", "yes", true], ["f", "toggle force"], ["u", "toggle unlock"], ["esc", "cancel"]],
    legend: [["esc", "close", true]],
    shell: [["⏎", "fell --dry-run", true], ["j", "scan --json | jq"]],
    scanning: [],
  };
  const renderKeys = () => {
    const v = st.confirm ? "confirm" : st.view;
    const arrows = v === "list" ? [["↑", ""], ["↓", ""]] : [];
    keysEl.innerHTML = arrows.concat(KEYSETS[v]).map(([k, l, p]) => `<button type="button" data-key="${esc(k)}" class="${p ? "primary" : ""}"><b>${esc(k)}</b>${esc(l)}</button>`).join("");
  };

  /* ---------- actions: a dry run, one at a time like the TUI's queue ---------- */
  const startScan = () => {
    clearTimeout(timer);
    st = Object.assign(fresh(), { group: st.group, sort: st.sort });
    render();
    timer = setTimeout(() => {
      st.view = "list";
      const first = rows().find((r) => r.w);
      st.cursorKey = first ? first.key : "";
      render();
    }, reduceMotion ? 0 : 700);
  };
  const next = () => {
    if (!st.queue.length) return render();
    const j = st.queue.shift(), w = find(j.id);
    st.status[j.id] = "working";
    render();
    timer = setTimeout(() => {
      st.outcomes.push({ action: j.action, w, estimate: estimate(j.action, w), status: "dry_run", stamp: stamp() });
      st.selected.delete(j.id);
      st.status[j.id] = j.action === "strip" ? "idle" : "done"; // a stripped worktree stays
      next();
    }, reduceMotion ? 0 : 450);
  };
  const ask = (action, rs, i) => {
    let ids = rs.filter((r) => r.w && st.selected.has(r.w.id)).map((r) => r.w.id);
    if (!ids.length && rs.length) ids = leaves(rs[i]);
    ids = ids.filter((id) => !["done", "working", "queued"].includes(st.status[id]));
    if (ids.length) st.confirm = { action, ids, force: false, unlock: false };
  };
  const toggle = (ids) => {
    ids = ids.filter((id) => st.status[id] !== "done" && st.status[id] !== "working");
    const all = ids.length > 0 && ids.every((id) => st.selected.has(id));
    ids.forEach((id) => (all ? st.selected.delete(id) : st.selected.add(id)));
  };

  /* ---------- keys (Model.key) ---------- */
  const key = (k) => {
    if (hint) hint.classList.add("gone");
    if (st.view === "scanning") return false;
    if (st.view === "shell") {
      if (k === "enter") { startScan(); return true; }
      if (k === "j") {
        st.shell.push([S("f", "$ "), S("", JQ)]);
        items().forEach((it) => st.shell.push([S("", JSON.stringify([it.id, it.name, it.safety.level]))]));
        render(); return true;
      }
      return false;
    }
    if (st.view === "legend") { st.view = "list"; render(); return true; }
    if (st.confirm) {
      if (k === "y" || k === "Y") {
        const { run } = plan();
        run.forEach((w) => { st.status[w.id] = "queued"; st.queue.push({ action: st.confirm.action, id: w.id }); });
        st.confirm = null;
        if (!Object.values(st.status).includes("working")) next(); else render();
        return true;
      }
      if (k === "f") st.confirm.force = !st.confirm.force;
      else if (k === "u") st.confirm.unlock = !st.confirm.unlock;
      else st.confirm = null;
      render(); return true;
    }
    const rs = rows();
    let i = cursor(rs);
    switch (k) {
      case "q": case "esc":
        if (busy()) return true;
        clearTimeout(timer);
        st.view = "shell"; st.shell = receipt(); render(); return true;
      case "?": st.view = "legend"; render(); return true;
      case "up": i--; break;
      case "down": i++; break;
      case "pgup": i -= bodyHeight(); break;
      case "pgdown": i += bodyHeight(); break;
      case "home": i = 0; break;
      case "end": i = rs.length - 1; break;
      case "tab": st.group = 1 - st.group; st.offset = 0; render(); return true;
      case "o": st.sort = (st.sort + 1) % 3; render(); return true;
      case "R": if (!busy()) startScan(); return true;
      case " ": case "space": if (rs.length) toggle(leaves(rs[i])); render(); return true;
      case "a": toggle(rs.filter((r) => r.w && r.w.safety.level !== "live").map((r) => r.w.id)); render(); return true;
      case "s": ask("strip", rs, i); render(); return true;
      case "r": case "d": ask("retire", rs, i); render(); return true;
      default: return false;
    }
    if (rs.length) { i = Math.max(0, Math.min(i, rs.length - 1)); st.cursorKey = rs[i].key; }
    render();
    return true;
  };
  const KEYMAP = { ArrowUp: "up", k: "up", ArrowDown: "down", j: "down", Enter: "enter", Escape: "esc", PageUp: "pgup", PageDown: "pgdown", Home: "home", End: "end", Tab: "tab" };
  term.addEventListener("keydown", (e) => {
    if (e.metaKey || e.ctrlKey || e.altKey) return;
    if (e.key === "Tab" && (e.shiftKey || st.view === "shell")) return; // let focus leave
    let k = KEYMAP[e.key] || (e.key.length === 1 ? e.key : null);
    if (st.view === "shell" && e.key === "j") k = "j";
    if (!k) return;
    if (key(k) !== false) e.preventDefault();
  });
  keysEl.addEventListener("click", (e) => {
    const b = e.target.closest("button[data-key]");
    if (!b) return;
    const map = { "↑": "up", "↓": "down", "⏎": "enter" };
    key(map[b.dataset.key] || b.dataset.key);
    term.focus({ preventScroll: true });
  });
  term.addEventListener("click", (e) => {
    term.focus({ preventScroll: true });
    const r = e.target.closest("[data-row]");
    if (st.view === "legend") { key("esc"); return; }
    if (!r || st.view !== "list" || st.confirm) return;
    const rs = rows(), i = +r.dataset.row;
    if (!rs[i]) return;
    if (rs[i].key === st.cursorKey) key("space"); // a second click selects, like space
    else { st.cursorKey = rs[i].key; render(); }
  });

  let rt;
  addEventListener("resize", () => { clearTimeout(rt); rt = setTimeout(render, 80); });

  startScan();
})();
