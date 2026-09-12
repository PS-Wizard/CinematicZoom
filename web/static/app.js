const $ = (sel, el = document) => el.querySelector(sel);

const state = {
  home: "",
  encoder: "",
  path: null,
  probe: null,
  zooms: [],
  selectedId: null,
  selectedPathId: null,
  drawing: null,
  dirty: false,
};

const video = $("#video");
const viewport = $("#viewport");
const rectEl = $("#rect");
const stage = $("#stage");

let dragging = null;

const HOLD_MS = 200;
const HOLD_MOVE_PX = 8;
const SAMPLE_DT = 0.08;
const SAMPLE_DIST = 0.025;

function fmtTime(s) {
  if (!Number.isFinite(s) || s < 0) s = 0;
  const m = Math.floor(s / 60);
  const sec = s % 60;
  return `${m}:${sec.toFixed(2).padStart(5, "0")}`;
}

function lerp(a, b, t) {
  return a + (b - a) * t;
}

function ease(p, kind) {
  p = Math.min(1, Math.max(0, p));
  switch (kind) {
    case "linear":
      return p;
    case "easeInCubic":
      return p * p * p;
    case "easeOutCubic":
      return 1 - (1 - p) ** 3;
    default:
      return p < 0.5 ? 4 * p * p * p : 1 - (-2 * p + 2) ** 3 / 2;
  }
}

function panEaseKind(z) {
  return z.panEasing || z.easing || "easeInOutCubic";
}

function uid() {
  return "z_" + Math.random().toString(36).slice(2, 8);
}

function duration() {
  return state.probe?.duration ?? video.duration ?? 0;
}

function selected() {
  return state.zooms.find((z) => z.id === state.selectedId) ?? null;
}

function zoomAt(t) {
  return state.zooms.find((z) => t >= z.inStart && t <= z.outEnd) ?? null;
}

function amountFor(z, t) {
  if (t <= z.inStart || t >= z.outEnd) return 0;
  if (t < z.inEnd) {
    const d = Math.max(z.inEnd - z.inStart, 0.0001);
    return ease((t - z.inStart) / d, z.easing);
  }
  if (t <= z.outStart) return 1;
  const d = Math.max(z.outEnd - z.outStart, 0.0001);
  return 1 - ease((t - z.outStart) / d, z.easing);
}

function normalizePath(z) {
  if (!z) return;
  if (!Array.isArray(z.path)) z.path = [];
  for (const p of z.path) {
    if (!p.id) p.id = uid();
    if (!Number.isFinite(p.w)) p.w = z.rect?.w ?? 0.5;
    if (!Number.isFinite(p.h)) p.h = p.w;
  }
  z.path.sort((a, b) => a.t - b.t);
}

function windowDist(a, b) {
  if (!a || !b) return Infinity;
  return Math.hypot(a.x - b.x, a.y - b.y) + Math.abs((a.w ?? 0) - (b.w ?? 0));
}

function sameWindow(a, b) {
  return windowDist(a, b) < 0.012;
}

function simplifyPath(z) {
  if (!z || !Array.isArray(z.path) || z.path.length < 3) return;
  normalizePath(z);
  const pts = z.path;
  const out = [];
  let i = 0;
  while (i < pts.length) {
    let j = i;
    while (j + 1 < pts.length && sameWindow(pts[i], pts[j + 1])) j++;
    out.push(pts[i]);
    if (j > i && pts[j].t - pts[i].t > 0.08) out.push(pts[j]);
    i = j + 1;
  }
  z.path = out;
}

function copyRect(p) {
  return { x: p.x, y: p.y, w: p.w, h: p.h };
}

function clampRect(r) {
  r.w = Math.min(1, Math.max(0.08, r.w));
  r.h = r.w;
  r.x = Math.min(1 - r.w, Math.max(0, r.x));
  r.y = Math.min(1 - r.h, Math.max(0, r.y));
  return r;
}

function rectAt(z, t) {
  normalizePath(z);
  const pts = z.path;
  if (!pts.length) return copyRect(z.rect);
  if (t < pts[0].t) return copyRect(z.rect);
  if (t <= pts[0].t || pts.length === 1) return copyRect(pts[0]);
  const last = pts[pts.length - 1];
  if (t >= last.t) return copyRect(last);
  for (let i = 1; i < pts.length; i++) {
    if (t > pts[i].t) continue;
    const a = pts[i - 1];
    const b = pts[i];
    const p = ease((t - a.t) / Math.max(b.t - a.t, 0.0001), panEaseKind(z));
    return {
      x: lerp(a.x, b.x, p),
      y: lerp(a.y, b.y, p),
      w: lerp(a.w, b.w, p),
      h: lerp(a.h, b.h, p),
    };
  }
  return copyRect(z.rect);
}

function displayRect(z) {
  if (state.drawing === z && dragging?.live) return copyRect(dragging.live);
  const t = Math.min(z.outEnd, Math.max(z.inStart, video.currentTime));
  return rectAt(z, t);
}

function applyPreview() {
  const t = video.currentTime;
  const z = zoomAt(t);
  if (!z || video.paused || state.drawing) {
    video.style.transform = "";
    return;
  }
  const a = amountFor(z, t);
  const r = rectAt(z, t);
  const nx = lerp(0, r.x, a);
  const ny = lerp(0, r.y, a);
  const nw = lerp(1, r.w, a);
  const nh = lerp(1, r.h, a);
  video.style.transform = `scale(${1 / nw}, ${1 / nh}) translate(${-nx * 100}%, ${-ny * 100}%)`;
}

function layoutViewport() {
  if (!state.probe) return;
  const ar = state.probe.width / state.probe.height;
  const sw = stage.clientWidth;
  const sh = stage.clientHeight;
  let w = sw;
  let h = sw / ar;
  if (h > sh) {
    h = sh;
    w = sh * ar;
  }
  viewport.style.width = `${Math.max(1, Math.floor(w))}px`;
  viewport.style.height = `${Math.max(1, Math.floor(h))}px`;
}

function renderRect() {
  const z = selected();
  const show = z && (video.paused || state.drawing === z);
  rectEl.classList.toggle("hidden", !show);
  rectEl.classList.toggle("drawing", !!(z && state.drawing === z));
  if (!show) return;
  const r = displayRect(z);
  rectEl.style.left = `${r.x * 100}%`;
  rectEl.style.top = `${r.y * 100}%`;
  rectEl.style.width = `${r.w * 100}%`;
  rectEl.style.height = `${r.h * 100}%`;
}

function renderPathDots() {
  const wrap = $("#path-dots");
  if (!wrap) return;
  wrap.innerHTML = "";
  const z = selected();
  if (!z || !video.paused) return;
  normalizePath(z);
  if (!z.path.length) return;
  const pts = pathCorners(z.path);
  for (const p of pts) {
    const d = document.createElement("div");
    d.className = "path-dot" + (p.id === state.selectedPathId ? " selected" : "");
    d.style.left = `${(p.x + p.w / 2) * 100}%`;
    d.style.top = `${(p.y + p.h / 2) * 100}%`;
    d.title = fmtTime(p.t);
    d.addEventListener("pointerdown", (ev) => {
      ev.preventDefault();
      ev.stopPropagation();
      state.selectedPathId = p.id;
      video.currentTime = p.t;
      sync();
    });
    wrap.appendChild(d);
  }
}

function pathCorners(path) {
  if (path.length <= 2) return path.slice();
  const out = [path[0]];
  for (let i = 1; i < path.length - 1; i++) {
    const a = out[out.length - 1];
    const b = path[i];
    const c = path[i + 1];
    if (sameWindow(a, b) && sameWindow(b, c)) continue;
    out.push(b);
  }
  out.push(path[path.length - 1]);
  if (out.length > 24) {
    const step = (out.length - 1) / 23;
    const slim = [out[0]];
    for (let i = 1; i < 23; i++) slim.push(out[Math.round(i * step)]);
    slim.push(out[out.length - 1]);
    return slim;
  }
  return out;
}

function renderList() {
  const ul = $("#zoom-list");
  ul.innerHTML = "";
  if (!state.zooms.length) {
    ul.innerHTML = `<li class="muted">No zooms yet</li>`;
    return;
  }
  for (const z of [...state.zooms].sort((a, b) => a.inStart - b.inStart)) {
    const li = document.createElement("li");
    if (z.id === state.selectedId) li.classList.add("selected");
    const mag = (1 / z.rect.w).toFixed(2);
    const pan = z.path?.length ? ` · ${z.path.length} steps` : "";
    li.innerHTML = `<div class="t">${fmtTime(z.inStart)} – ${fmtTime(z.outEnd)}</div>
      <div class="sub">${mag}×${pan} · in ${(z.inEnd - z.inStart).toFixed(2)}s · out ${(z.outEnd - z.outStart).toFixed(2)}s</div>`;
    li.addEventListener("click", () => {
      state.selectedId = z.id;
      state.selectedPathId = null;
      video.currentTime = z.inStart;
      sync();
    });
    ul.appendChild(li);
  }
}

function renderInspector() {
  const z = selected();
  const box = $("#inspector");
  box.classList.toggle("hidden", !z);
  if (!z) return;
  $("#in-dur").value = (z.inEnd - z.inStart).toFixed(2);
  $("#hold-dur").value = Math.max(0, z.outStart - z.inEnd).toFixed(2);
  $("#out-dur").value = (z.outEnd - z.outStart).toFixed(2);
  $("#easing").value = z.easing || "easeInOutCubic";
  normalizePath(z);
  const panMeta = $("#pan-meta");
  if (panMeta) {
    if (state.drawing) panMeta.textContent = "drawing · hold still to stay, move to pan, resize if you want";
    else if (z.path.length) panMeta.textContent = `${z.path.length} steps · ${fmtTime(z.path[0].t)} → ${fmtTime(z.path[z.path.length - 1].t)} · hold the window to draw`;
    else panMeta.textContent = "hold the window while video plays to draw its path";
  }
  const delPan = $("#btn-delete-pan");
  if (delPan) delPan.classList.toggle("hidden", z.path.length === 0);
}

function renderTimeline() {
  const dur = duration();
  const regions = $("#tl-regions");
  regions.innerHTML = "";
  if (!dur) return;
  for (const z of state.zooms) {
    const el = document.createElement("div");
    el.className = "tl-region" + (z.id === state.selectedId ? " selected" : "");
    el.style.left = `${(z.inStart / dur) * 100}%`;
    el.style.width = `${((z.outEnd - z.inStart) / dur) * 100}%`;
    el.dataset.id = z.id;
    const left = document.createElement("div");
    left.className = "tl-edge left";
    const right = document.createElement("div");
    right.className = "tl-edge right";
    el.append(left, right);
    regions.appendChild(el);
  }
  const labels = $("#tl-labels");
  labels.innerHTML = `<span>0:00</span><span>${fmtTime(dur)}</span>`;
  renderPanTrack();
  updatePlayhead();
}

function renderPanTrack() {
  const keys = $("#pan-keys");
  if (!keys) return;
  keys.innerHTML = "";
  const dur = duration();
  const z = selected();
  if (!dur || !z) return;
  normalizePath(z);
  const span = document.createElement("div");
  span.className = "pan-span";
  span.style.left = `${(z.inStart / dur) * 100}%`;
  span.style.width = `${((z.outEnd - z.inStart) / dur) * 100}%`;
  keys.appendChild(span);
  if (z.path.length) {
    const first = z.path[0];
    const last = z.path[z.path.length - 1];
    const seg = document.createElement("div");
    seg.className = "pan-seg" + (state.drawing === z ? " selected" : "");
    seg.style.left = `${(first.t / dur) * 100}%`;
    seg.style.width = `${Math.max(0, last.t - first.t) / dur * 100}%`;
    keys.appendChild(seg);
    const left = document.createElement("div");
    left.className = "tl-edge left";
    const right = document.createElement("div");
    right.className = "tl-edge right";
    seg.append(left, right);
    for (const p of pathCorners(z.path)) {
      const tick = document.createElement("div");
      tick.className = "pan-tick";
      tick.style.left = `${(p.t / dur) * 100}%`;
      tick.title = fmtTime(p.t);
      keys.appendChild(tick);
    }
  }
}

function updatePlayhead() {
  const dur = duration();
  if (!dur) return;
  const pct = `${(video.currentTime / dur) * 100}%`;
  $("#playhead").style.left = pct;
  const panHead = $("#playhead-pan");
  if (panHead) panHead.style.left = pct;
  $("#clock").textContent = fmtTime(video.currentTime);
}

function sync() {
  applyPreview();
  renderRect();
  renderPathDots();
  renderList();
  renderInspector();
  renderTimeline();
  $("#btn-play").textContent = video.paused ? "Play" : "Pause";
}

function markDirty() {
  state.dirty = true;
}

function clampZoom(z) {
  const dur = duration();
  const minIn = 0.05;
  const minHold = 0.05;
  const minOut = 0.05;
  z.inStart = Math.max(0, z.inStart);
  z.outEnd = Math.min(dur, z.outEnd);
  if (z.inEnd < z.inStart + minIn) z.inEnd = z.inStart + minIn;
  if (z.outStart < z.inEnd + minHold) z.outStart = z.inEnd + minHold;
  if (z.outEnd < z.outStart + minOut) z.outEnd = z.outStart + minOut;
  if (z.outEnd > dur) {
    const extra = z.outEnd - dur;
    z.inStart = Math.max(0, z.inStart - extra);
    z.inEnd -= extra;
    z.outStart -= extra;
    z.outEnd = dur;
    for (const p of z.path || []) p.t -= extra;
  }
  const r = z.rect;
  r.w = Math.min(1, Math.max(0.08, r.w));
  r.h = r.w;
  r.x = Math.min(1 - r.w, Math.max(0, r.x));
  r.y = Math.min(1 - r.h, Math.max(0, r.y));
  normalizePath(z);
  for (const p of z.path) {
    p.t = Math.min(z.outEnd, Math.max(z.inStart, p.t));
    p.w = Math.min(1, Math.max(0.08, Number.isFinite(p.w) ? p.w : r.w));
    p.h = p.w;
    p.x = Math.min(1 - p.w, Math.max(0, p.x));
    p.y = Math.min(1 - p.h, Math.max(0, p.y));
  }
  z.path.sort((a, b) => a.t - b.t);
}

function addZoom() {
  if (!state.probe) return;
  const t = video.currentTime;
  const inDur = 0.5;
  const hold = 2;
  const outDur = 0.5;
  const z = {
    id: uid(),
    inStart: t,
    inEnd: t + inDur,
    outStart: t + inDur + hold,
    outEnd: t + inDur + hold + outDur,
    rect: { x: 0.25, y: 0.25, w: 0.5, h: 0.5 },
    path: [],
    easing: "easeInOutCubic",
    panEasing: "linear",
  };
  clampZoom(z);
  state.zooms.push(z);
  state.selectedId = z.id;
  state.selectedPathId = null;
  markDirty();
  sync();
}

function panEnds(z) {
  normalizePath(z);
  if (!z.path.length) return null;
  return { start: z.path[0], end: z.path[z.path.length - 1] };
}

function extendZoomTo(z, t) {
  const dur = duration();
  const outDur = Math.max(0.05, z.outEnd - z.outStart);
  if (t > z.outEnd - 0.12) {
    z.outEnd = Math.min(dur, t + 0.35);
    z.outStart = Math.max(z.inEnd + 0.05, z.outEnd - outDur);
  }
}

function startDraw(z) {
  if (!z || !dragging) return;
  const dur = duration();
  let t = video.currentTime;
  if (t < z.inStart) t = z.inStart;
  if (t > z.outEnd - 0.08) t = Math.max(z.inStart, z.outEnd - 0.08);
  if (Math.abs(video.currentTime - t) > 0.02) video.currentTime = t;
  const live = dragging.live ? clampRect(dragging.live) : clampRect(copyRect(displayRect(z)));
  dragging.live = live;
  dragging.point = live;
  dragging.moving = false;
  dragging.lastMoveT = t;
  dragging.lastSampleT = t;
  normalizePath(z);
  z.path = z.path.filter((p) => p.t < t - 0.001);
  z.path.push({ id: uid(), t, x: live.x, y: live.y, w: live.w, h: live.h });
  if (!z.path.length || t <= z.path[0].t) z.rect = copyRect(live);
  z.panEasing = z.panEasing || "linear";
  state.drawing = z;
  state.selectedId = z.id;
  dragging.kind = "draw";
  markDirty();
  sync();
  if (video.paused && t < dur - 0.02) video.play().catch(() => {});
}

function recordWindow() {
  const z = state.drawing;
  if (!z || !dragging?.live) return;
  const live = clampRect(dragging.live);
  const t = video.currentTime;
  extendZoomTo(z, t);
  normalizePath(z);
  let last = z.path[z.path.length - 1];
  if (!last) {
    z.path.push({ id: uid(), t, ...copyRect(live) });
    dragging.lastMoveT = t;
    dragging.lastSampleT = t;
    return;
  }
  if (sameWindow(last, live)) return;
  if (!dragging.moving) {
    if (t - last.t > 0.05) {
      z.path.push({
        id: uid(),
        t,
        x: last.x,
        y: last.y,
        w: last.w,
        h: last.h,
      });
    }
    dragging.moving = true;
    z.path.push({ id: uid(), t: t + 0.001, ...copyRect(live) });
    dragging.lastMoveT = t;
    dragging.lastSampleT = t;
    return;
  }
  last = z.path[z.path.length - 1];
  const dt = t - (dragging.lastSampleT ?? last.t);
  if (dt >= SAMPLE_DT && windowDist(last, live) >= SAMPLE_DIST) {
    z.path.push({ id: uid(), t, ...copyRect(live) });
    dragging.lastSampleT = t;
  }
  dragging.lastMoveT = t;
}

function tickDraw() {
  const z = state.drawing;
  if (!z) return;
  const t = video.currentTime;
  const dur = duration();
  extendZoomTo(z, t);
  if (dragging?.moving && t - (dragging.lastMoveT ?? t) > 0.14) dragging.moving = false;
  renderPanTrack();
  renderInspector();
  renderRect();
  renderPathDots();
  if (t >= dur - 0.02) stopDraw();
}

function stopDraw() {
  const z = state.drawing;
  if (!z) return;
  const t = video.currentTime;
  if (dragging?.live) {
    const live = clampRect(dragging.live);
    normalizePath(z);
    const last = z.path[z.path.length - 1];
    if (!last) {
      z.path.push({ id: uid(), t, ...copyRect(live) });
    } else if (sameWindow(last, live)) {
      if (t - last.t > 0.08) z.path.push({ id: uid(), t, ...copyRect(live) });
      else last.t = Math.max(last.t, t);
    } else if (t - last.t >= 0.04) {
      z.path.push({ id: uid(), t, ...copyRect(live) });
    } else {
      last.x = live.x;
      last.y = live.y;
      last.w = live.w;
      last.h = live.h;
      last.t = Math.max(last.t, t);
    }
  }
  state.drawing = null;
  simplifyPath(z);
  clampZoom(z);
  if (!video.paused) video.pause();
  markDirty();
  sync();
}

function commitPausedWindow(z, live) {
  clampRect(live);
  normalizePath(z);
  if (!z.path.length) {
    Object.assign(z.rect, copyRect(live));
    return;
  }
  const t = video.currentTime;
  if (t < z.path[0].t - 0.04) {
    Object.assign(z.rect, copyRect(live));
    return;
  }
  let best = z.path[0];
  for (const p of z.path) {
    if (Math.abs(p.t - t) < Math.abs(best.t - t)) best = p;
  }
  best.x = live.x;
  best.y = live.y;
  best.w = live.w;
  best.h = live.h;
}

function clearHoldTimer() {
  if (dragging?.holdTimer) {
    clearTimeout(dragging.holdTimer);
    dragging.holdTimer = null;
  }
}

function pointerMoved(ev, from) {
  const dx = ev.clientX - from.clientX;
  const dy = ev.clientY - from.clientY;
  return dx * dx + dy * dy > HOLD_MOVE_PX * HOLD_MOVE_PX;
}

function deleteSelectedPan() {
  const z = selected();
  if (!z || !z.path?.length) return false;
  z.path = [];
  state.selectedPathId = null;
  markDirty();
  sync();
  return true;
}

function clearPans() {
  const z = selected();
  if (!z || !z.path?.length) return;
  z.path = [];
  state.selectedPathId = null;
  markDirty();
  sync();
}

function applyInspector() {
  const z = selected();
  if (!z) return;
  const inDur = Number($("#in-dur").value);
  const hold = Number($("#hold-dur").value);
  const outDur = Number($("#out-dur").value);
  z.easing = $("#easing").value;
  z.panEasing = z.panEasing || "linear";
  z.inEnd = z.inStart + inDur;
  z.outStart = z.inEnd + hold;
  z.outEnd = z.outStart + outDur;
  clampZoom(z);
  markDirty();
  sync();
}

function toNorm(ev) {
  const b = viewport.getBoundingClientRect();
  return {
    x: (ev.clientX - b.left) / b.width,
    y: (ev.clientY - b.top) / b.height,
  };
}

function bindOverlay() {
  rectEl.addEventListener("pointerdown", (ev) => {
    const z = selected();
    if (!z || ev.button !== 0) return;
    ev.preventDefault();
    ev.stopPropagation();
    const handle = ev.target.dataset.handle || "move";
    const live = clampRect(copyRect(displayRect(z)));
    dragging = {
      kind: "rectPending",
      handle,
      start: toNorm(ev),
      orig: copyRect(live),
      live,
      point: live,
      clientX: ev.clientX,
      clientY: ev.clientY,
      holdTimer: setTimeout(() => {
        if (dragging?.kind !== "rectPending") return;
        startDraw(z);
      }, HOLD_MS),
    };
    rectEl.setPointerCapture(ev.pointerId);
  });
}

function applyRectDrag(ev, z) {
  const p = toNorm(ev);
  const o = dragging.orig;
  const target = dragging.point;
  const min = 0.12;
  if (dragging.handle === "move") {
    target.x = o.x + (p.x - dragging.start.x);
    target.y = o.y + (p.y - dragging.start.y);
  } else {
    const brx = o.x + o.w;
    const bry = o.y + o.h;
    let size = o.w;
    switch (dragging.handle) {
      case "se":
        size = Math.max(p.x - o.x, p.y - o.y);
        size = Math.min(size, 1 - o.x, 1 - o.y);
        target.x = o.x;
        target.y = o.y;
        break;
      case "nw":
        size = Math.max(brx - p.x, bry - p.y);
        size = Math.min(size, brx, bry);
        target.x = brx - size;
        target.y = bry - size;
        break;
      case "ne":
        size = Math.max(p.x - o.x, bry - p.y);
        size = Math.min(size, 1 - o.x, bry);
        target.x = o.x;
        target.y = bry - size;
        break;
      case "sw":
        size = Math.max(brx - p.x, p.y - o.y);
        size = Math.min(size, brx, 1 - o.y);
        target.x = brx - size;
        target.y = o.y;
        break;
    }
    target.w = Math.max(min, size);
    target.h = target.w;
  }
  clampRect(target);
}

function onPointerMove(ev) {
  if (!dragging) return;

  if (dragging.kind === "rectPending") {
    if (!pointerMoved(ev, dragging)) return;
    clearHoldTimer();
    dragging.kind = "rect";
  }

  const z = selected();
  if (!z) return;

  if (dragging.kind === "draw") {
    applyRectDrag(ev, z);
    recordWindow();
    renderRect();
    renderPanTrack();
    renderPathDots();
    return;
  }

  if (dragging.kind !== "rect") return;
  applyRectDrag(ev, z);
  commitPausedWindow(z, dragging.live);
  renderRect();
  renderList();
  renderPathDots();
  renderPanTrack();
}

function onPointerUp(ev) {
  if (ev && ev.button && ev.button !== 0) return;
  clearHoldTimer();
  if (dragging?.kind === "draw" || state.drawing) stopDraw();
  else if (dragging?.kind === "rect" && selected() && dragging.live) {
    commitPausedWindow(selected(), dragging.live);
    clampZoom(selected());
  }
  if (dragging && dragging.kind !== "seek") markDirty();
  dragging = null;
}

function bindTimeline() {
  const track = $("#tl-track");
  track.addEventListener("pointerdown", (ev) => {
    if (state.drawing) return;
    const dur = duration();
    if (!dur) return;
    const region = ev.target.closest(".tl-region");
    const edge = ev.target.classList.contains("tl-edge") ? ev.target : null;
    const box = track.getBoundingClientRect();
    const tAt = (clientX) => {
      const t = ((clientX - box.left) / box.width) * dur;
      return Math.min(dur, Math.max(0, t));
    };

    if (region) {
      const z = state.zooms.find((x) => x.id === region.dataset.id);
      state.selectedId = z.id;
      state.selectedPathId = null;
      normalizePath(z);
      const orig = {
        inStart: z.inStart,
        inEnd: z.inEnd,
        outStart: z.outStart,
        outEnd: z.outEnd,
        path: z.path.map((p) => p.t),
      };
      dragging = {
        kind: edge ? (edge.classList.contains("left") ? "edgeL" : "edgeR") : "moveZ",
        z,
        orig,
        startX: ev.clientX,
        box,
        dur,
      };
      sync();
    } else {
      video.currentTime = tAt(ev.clientX);
      dragging = { kind: "seek", box, dur };
      updatePlayhead();
      applyPreview();
      renderRect();
    }
    track.setPointerCapture(ev.pointerId);
  });

  track.addEventListener("pointermove", (ev) => {
    if (!dragging) return;
    const dur = dragging.dur ?? duration();
    const box = dragging.box ?? track.getBoundingClientRect();
    const tAt = (clientX) => {
      const t = ((clientX - box.left) / box.width) * dur;
      return Math.min(dur, Math.max(0, t));
    };
    if (dragging.kind === "seek") {
      video.currentTime = tAt(ev.clientX);
      updatePlayhead();
      applyPreview();
      renderRect();
      renderPathDots();
      return;
    }
    if (!dragging.z) return;
    const dt = ((ev.clientX - dragging.startX) / box.width) * dur;
    const z = dragging.z;
    const o = dragging.orig;
    const span = o.outEnd - o.inStart;
    if (dragging.kind === "moveZ") {
      z.inStart = o.inStart + dt;
      z.inEnd = o.inEnd + dt;
      z.outStart = o.outStart + dt;
      z.outEnd = o.outEnd + dt;
      if (z.inStart < 0) {
        z.inStart = 0;
        z.inEnd = o.inEnd - o.inStart;
        z.outStart = o.outStart - o.inStart;
        z.outEnd = o.outEnd - o.inStart;
      }
      if (z.outEnd > dur) {
        const shift = z.outEnd - dur;
        z.inStart -= shift;
        z.inEnd -= shift;
        z.outStart -= shift;
        z.outEnd = dur;
      }
      const pathShift = z.inStart - o.inStart;
      normalizePath(z);
      for (let i = 0; i < z.path.length; i++) {
        z.path[i].t = (o.path[i] ?? z.path[i].t) + pathShift;
      }
    } else if (dragging.kind === "edgeL") {
      z.inStart = Math.min(o.outStart - 0.2, Math.max(0, o.inStart + dt));
      const inDur = o.inEnd - o.inStart;
      z.inEnd = z.inStart + inDur;
      if (z.inEnd > z.outStart) z.inEnd = z.outStart;
    } else if (dragging.kind === "edgeR") {
      z.outEnd = Math.max(o.inEnd + 0.2, Math.min(dur, o.outEnd + dt));
      const outDur = o.outEnd - o.outStart;
      z.outStart = z.outEnd - outDur;
      if (z.outStart < z.inEnd) z.outStart = z.inEnd;
    }
    clampZoom(z);
    renderTimeline();
    renderList();
    renderInspector();
    renderRect();
    renderPathDots();
  });
}

function bindPanTrack() {
  const track = $("#pan-track");
  if (!track) return;
  const tAt = (ev, box, dur) => {
    const t = ((ev.clientX - box.left) / box.width) * dur;
    return Math.min(dur, Math.max(0, t));
  };
  track.addEventListener("pointerdown", (ev) => {
    if (state.drawing) return;
    const dur = duration();
    if (!dur) return;
    const box = track.getBoundingClientRect();
    const seg = ev.target.closest(".pan-seg");
    const edge = ev.target.classList.contains("tl-edge") ? ev.target : null;
    const z = selected();
    if (seg && z) {
      const ends = panEnds(z);
      if (!ends) return;
      dragging = {
        kind: edge ? (edge.classList.contains("left") ? "panL" : "panR") : "movePan",
        z,
        orig: {
          start: ends.start.t,
          end: ends.end.t,
          times: z.path.map((p) => p.t),
        },
        startX: ev.clientX,
        box,
        dur,
      };
      track.setPointerCapture(ev.pointerId);
      return;
    }
    video.currentTime = tAt(ev, box, dur);
    dragging = { kind: "seek", box, dur };
    track.setPointerCapture(ev.pointerId);
    updatePlayhead();
    applyPreview();
    renderRect();
    renderPathDots();
  });
  track.addEventListener("pointermove", (ev) => {
    if (!dragging) return;
    const dur = dragging.dur ?? duration();
    const box = dragging.box ?? track.getBoundingClientRect();
    if (dragging.kind === "seek") {
      video.currentTime = tAt(ev, box, dur);
      updatePlayhead();
      applyPreview();
      renderRect();
      renderPathDots();
      return;
    }
    if (!dragging.z || !["panL", "panR", "movePan"].includes(dragging.kind)) return;
    const dt = ((ev.clientX - dragging.startX) / box.width) * dur;
    const z = dragging.z;
    const ends = panEnds(z);
    if (!ends) return;
    const o = dragging.orig;
    const span = o.end - o.start;
    let s = o.start;
    let e = o.end;
    if (dragging.kind === "movePan") {
      s = o.start + dt;
      e = o.end + dt;
      if (s < z.inStart) {
        s = z.inStart;
        e = s + span;
      }
      if (e > z.outEnd) {
        e = z.outEnd;
        s = e - span;
      }
    } else if (dragging.kind === "panL") {
      s = Math.min(o.end - 0.05, Math.max(z.inStart, o.start + dt));
    } else if (dragging.kind === "panR") {
      e = Math.max(o.start + 0.05, Math.min(z.outEnd, o.end + dt));
    }
    for (let i = 0; i < z.path.length; i++) {
      const u = (o.times[i] - o.start) / Math.max(span, 0.0001);
      z.path[i].t = s + u * (e - s);
    }
    renderPanTrack();
    renderInspector();
    renderRect();
  });
}

function tick() {
  updatePlayhead();
  if (state.drawing) tickDraw();
  applyPreview();
  if (!video.paused) requestAnimationFrame(tick);
}

async function api(url, opts) {
  const res = await fetch(url, opts);
  if (!res.ok) {
    let msg = res.statusText;
    try {
      const j = await res.json();
      if (j.error) msg = j.error;
    } catch {}
    throw new Error(msg);
  }
  const ct = res.headers.get("content-type") || "";
  if (ct.includes("json") && !ct.includes("ndjson")) return res.json();
  return res;
}

async function openPath(path) {
  const probe = await api(`/api/probe?path=${encodeURIComponent(path)}`);
  let project = { zooms: [] };
  try {
    project = await api(`/api/project?path=${encodeURIComponent(path)}`);
  } catch {}
  state.path = probe.path;
  state.probe = probe;
  state.zooms = Array.isArray(project.zooms) ? project.zooms : [];
  for (const z of state.zooms) normalizePath(z);
  state.selectedId = state.zooms[0]?.id ?? null;
  state.selectedPathId = null;
  state.dirty = false;
  video.src = `/media?path=${encodeURIComponent(probe.path)}`;
  $("#empty").classList.add("hidden");
  viewport.classList.remove("hidden");
  $("#file-label").textContent = probe.path;
  $("#clock-dur").textContent = `/ ${fmtTime(probe.duration)}`;
  $("#btn-save").disabled = false;
  $("#btn-export").disabled = false;
  $("#btn-add").disabled = false;
  $("#btn-play").disabled = false;
  layoutViewport();
  sync();
}

async function saveProject() {
  if (!state.path) return;
  const r = await api("/api/project", {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ source: state.path, zooms: state.zooms }),
  });
  state.dirty = false;
  $("#file-label").textContent = state.path + " · saved";
  setTimeout(() => {
    if (state.path) $("#file-label").textContent = state.path;
  }, 1200);
  return r;
}

async function browse(path) {
  const data = await api(`/api/fs?path=${encodeURIComponent(path)}`);
  $("#fs-path").value = data.path;
  $("#fs-up").disabled = !data.parent;
  $("#fs-up").dataset.parent = data.parent || "";
  const ul = $("#fs-list");
  ul.innerHTML = "";
  for (const e of data.entries) {
    const li = document.createElement("li");
    const size = e.dir ? "" : formatSize(e.size);
    li.innerHTML = `<span class="${e.dir ? "dir" : ""}">${e.name}</span><span class="muted">${size}</span>`;
    li.addEventListener("click", async () => {
      if (e.dir) browse(e.path);
      else {
        $("#dlg-open").close();
        await openPath(e.path);
      }
    });
    ul.appendChild(li);
  }
}

function formatSize(n) {
  if (!n) return "";
  const u = ["B", "KB", "MB", "GB"];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < u.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(i ? 1 : 0)} ${u[i]}`;
}

function defaultOutput() {
  if (!state.path) return "";
  const i = state.path.lastIndexOf(".");
  const base = i >= 0 ? state.path.slice(0, i) : state.path;
  return base + "_cinematic.mp4";
}

async function runExport() {
  const output = $("#export-path").value.trim();
  const btn = $("#btn-run-export");
  btn.disabled = true;
  $("#export-log").textContent = "";
  $("#progress-bar").style.width = "0%";
  $("#export-status").textContent = "Starting…";
  try {
    const res = await fetch("/api/export", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        source: state.path,
        output,
        zooms: state.zooms,
      }),
    });
    if (!res.ok && !res.body) {
      throw new Error(await res.text());
    }
    const reader = res.body.getReader();
    const dec = new TextDecoder();
    let buf = "";
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      buf += dec.decode(value, { stream: true });
      const lines = buf.split("\n");
      buf = lines.pop();
      for (const line of lines) {
        if (!line.trim()) continue;
        const msg = JSON.parse(line);
        if (msg.type === "progress") {
          const pct = Math.round((msg.ratio || 0) * 100);
          $("#progress-bar").style.width = `${pct}%`;
          $("#export-status").textContent = `${pct}%  ${fmtTime(msg.time || 0)}`;
        } else if (msg.type === "log") {
          const pre = $("#export-log");
          pre.textContent += msg.line + "\n";
          pre.scrollTop = pre.scrollHeight;
        } else if (msg.type === "start") {
          $("#export-status").textContent = `encoding with ${msg.encoder}`;
        } else if (msg.type === "done") {
          $("#progress-bar").style.width = "100%";
          $("#export-status").textContent = `done · ${msg.output}`;
        } else if (msg.type === "error") {
          $("#export-status").textContent = msg.message;
        }
      }
    }
  } catch (err) {
    $("#export-status").textContent = err.message;
  } finally {
    btn.disabled = false;
  }
}

function bindKeys() {
  document.addEventListener("keydown", (ev) => {
    const tag = ev.target.tagName;
    if (tag === "INPUT" || tag === "SELECT" || tag === "TEXTAREA") return;
    if (!state.probe) return;
    if (ev.code === "Space") {
      ev.preventDefault();
      togglePlay();
    } else if (ev.key === "n" || ev.key === "N") {
      addZoom();
    } else if (ev.key === "Delete" || ev.key === "Backspace") {
      if (deleteSelectedPan()) return;
      const z = selected();
      if (!z) return;
      state.zooms = state.zooms.filter((x) => x.id !== z.id);
      state.selectedId = null;
      state.selectedPathId = null;
      markDirty();
      sync();
    } else if (ev.key === "ArrowLeft") {
      ev.preventDefault();
      video.currentTime = Math.max(0, video.currentTime - (ev.shiftKey ? 5 : 1));
      updatePlayhead();
      applyPreview();
      renderRect();
      renderPathDots();
    } else if (ev.key === "ArrowRight") {
      ev.preventDefault();
      video.currentTime = Math.min(duration(), video.currentTime + (ev.shiftKey ? 5 : 1));
      updatePlayhead();
      applyPreview();
      renderRect();
      renderPathDots();
    }
  });
}

function togglePlay() {
  if (!state.probe) return;
  if (state.drawing) {
    stopDraw();
    return;
  }
  if (video.paused) video.play();
  else video.pause();
}

async function main() {
  const meta = await api("/api/meta");
  state.home = meta.home;
  state.encoder = meta.encoder;
  $("#encoder").textContent = meta.encoder;

  $("#btn-open").addEventListener("click", () => {
    $("#dlg-open").showModal();
    browse(state.path ? state.path.replace(/\/[^/]+$/, "") : meta.start);
  });
  $("#btn-open-2").addEventListener("click", () => $("#btn-open").click());
  $("#fs-up").addEventListener("click", () => {
    const p = $("#fs-up").dataset.parent;
    if (p) browse(p);
  });
  $("#fs-go").addEventListener("click", () => browse($("#fs-path").value.trim()));
  $("#fs-path").addEventListener("keydown", (ev) => {
    if (ev.key === "Enter") browse($("#fs-path").value.trim());
  });
  document.querySelectorAll("[data-close]").forEach((b) => {
    b.addEventListener("click", () => b.closest("dialog").close());
  });

  $("#btn-add").addEventListener("click", addZoom);
  $("#btn-delete-pan").addEventListener("click", () => clearPans());
  $("#btn-delete").addEventListener("click", () => {
    const z = selected();
    if (!z) return;
    state.zooms = state.zooms.filter((x) => x.id !== z.id);
    state.selectedId = null;
    state.selectedPathId = null;
    markDirty();
    sync();
  });
  ["in-dur", "hold-dur", "out-dur", "easing"].forEach((id) => {
    $(`#${id}`).addEventListener("change", applyInspector);
  });
  $("#btn-save").addEventListener("click", () => saveProject().catch((e) => alert(e.message)));
  $("#btn-export").addEventListener("click", () => {
    $("#export-path").value = defaultOutput();
    $("#progress-bar").style.width = "0%";
    $("#export-status").textContent = `Ready · ${state.encoder}`;
    $("#export-log").textContent = "";
    $("#dlg-export").showModal();
  });
  $("#btn-run-export").addEventListener("click", runExport);
  $("#btn-play").addEventListener("click", togglePlay);

  video.addEventListener("play", () => {
    renderRect();
    renderPathDots();
    requestAnimationFrame(tick);
    $("#btn-play").textContent = "Pause";
  });
  video.addEventListener("pause", () => {
    if (state.drawing) stopDraw();
    $("#btn-play").textContent = "Play";
    applyPreview();
    renderRect();
    renderPathDots();
  });
  video.addEventListener("timeupdate", () => {
    if (video.paused) {
      updatePlayhead();
      applyPreview();
      renderRect();
      renderPathDots();
    }
  });
  video.addEventListener("click", () => {
    if (state.drawing) return;
    togglePlay();
  });

  bindOverlay();
  bindTimeline();
  bindPanTrack();
  bindKeys();
  window.addEventListener("pointermove", onPointerMove);
  window.addEventListener("pointerup", onPointerUp);
  window.addEventListener("pointercancel", onPointerUp);
  window.addEventListener("resize", layoutViewport);
  new ResizeObserver(layoutViewport).observe(stage);
}

main().catch((err) => {
  console.error(err);
  alert(err.message);
});
