const $ = (sel, el = document) => el.querySelector(sel);

const state = {
  home: "",
  encoder: "",
  path: null,
  probe: null,
  zooms: [],
  selectedId: null,
  dirty: false,
};

const video = $("#video");
const viewport = $("#viewport");
const rectEl = $("#rect");
const stage = $("#stage");

let dragging = null;

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
  if (kind === "linear") return p;
  return p < 0.5 ? 4 * p * p * p : 1 - (-2 * p + 2) ** 3 / 2;
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

function applyPreview() {
  const t = video.currentTime;
  const z = zoomAt(t);
  if (!z || video.paused) {
    video.style.transform = "";
    return;
  }
  const a = amountFor(z, t);
  const nx = lerp(0, z.rect.x, a);
  const ny = lerp(0, z.rect.y, a);
  const nw = lerp(1, z.rect.w, a);
  const nh = lerp(1, z.rect.h, a);
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
  const show = z && video.paused;
  rectEl.classList.toggle("hidden", !show);
  if (!show) return;
  rectEl.style.left = `${z.rect.x * 100}%`;
  rectEl.style.top = `${z.rect.y * 100}%`;
  rectEl.style.width = `${z.rect.w * 100}%`;
  rectEl.style.height = `${z.rect.h * 100}%`;
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
    li.innerHTML = `<div class="t">${fmtTime(z.inStart)} – ${fmtTime(z.outEnd)}</div>
      <div class="sub">${mag}× · in ${(z.inEnd - z.inStart).toFixed(2)}s · out ${(z.outEnd - z.outStart).toFixed(2)}s</div>`;
    li.addEventListener("click", () => {
      state.selectedId = z.id;
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
  updatePlayhead();
}

function updatePlayhead() {
  const dur = duration();
  if (!dur) return;
  $("#playhead").style.left = `${(video.currentTime / dur) * 100}%`;
  $("#clock").textContent = fmtTime(video.currentTime);
}

function sync() {
  applyPreview();
  renderRect();
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
  }
  const r = z.rect;
  r.w = Math.min(1, Math.max(0.08, r.w));
  r.h = r.w;
  r.x = Math.min(1 - r.w, Math.max(0, r.x));
  r.y = Math.min(1 - r.h, Math.max(0, r.y));
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
    easing: "easeInOutCubic",
  };
  clampZoom(z);
  state.zooms.push(z);
  state.selectedId = z.id;
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
    if (!z) return;
    ev.preventDefault();
    ev.stopPropagation();
    const handle = ev.target.dataset.handle || "move";
    const start = toNorm(ev);
    dragging = {
      kind: "rect",
      handle,
      start,
      orig: { ...z.rect },
    };
    rectEl.setPointerCapture(ev.pointerId);
  });
}

function onPointerMove(ev) {
  if (!dragging) return;
  const z = selected();
  if (!z || dragging.kind !== "rect") return;
  const p = toNorm(ev);
  const o = dragging.orig;
  const min = 0.12;
  if (dragging.handle === "move") {
    const dx = p.x - dragging.start.x;
    const dy = p.y - dragging.start.y;
    z.rect.x = o.x + dx;
    z.rect.y = o.y + dy;
  } else {
    const brx = o.x + o.w;
    const bry = o.y + o.h;
    let size = o.w;
    switch (dragging.handle) {
      case "se":
        size = Math.max(p.x - o.x, p.y - o.y);
        size = Math.min(size, 1 - o.x, 1 - o.y);
        z.rect.x = o.x;
        z.rect.y = o.y;
        break;
      case "nw":
        size = Math.max(brx - p.x, bry - p.y);
        size = Math.min(size, brx, bry);
        z.rect.x = brx - size;
        z.rect.y = bry - size;
        break;
      case "ne":
        size = Math.max(p.x - o.x, bry - p.y);
        size = Math.min(size, 1 - o.x, bry);
        z.rect.x = o.x;
        z.rect.y = bry - size;
        break;
      case "sw":
        size = Math.max(brx - p.x, p.y - o.y);
        size = Math.min(size, brx, 1 - o.y);
        z.rect.x = brx - size;
        z.rect.y = o.y;
        break;
    }
    z.rect.w = Math.max(min, size);
    z.rect.h = z.rect.w;
  }
  clampZoom(z);
  renderRect();
  renderList();
}

function onPointerUp() {
  if (dragging && dragging.kind !== "seek") markDirty();
  dragging = null;
}

function bindTimeline() {
  const track = $("#tl-track");
  track.addEventListener("pointerdown", (ev) => {
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
      const orig = { inStart: z.inStart, inEnd: z.inEnd, outStart: z.outStart, outEnd: z.outEnd };
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
  });
}

function tick() {
  updatePlayhead();
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
  state.selectedId = state.zooms[0]?.id ?? null;
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
      const z = selected();
      if (!z) return;
      state.zooms = state.zooms.filter((x) => x.id !== z.id);
      state.selectedId = null;
      markDirty();
      sync();
    } else if (ev.key === "ArrowLeft") {
      ev.preventDefault();
      video.currentTime = Math.max(0, video.currentTime - (ev.shiftKey ? 5 : 1));
      updatePlayhead();
      applyPreview();
      renderRect();
    } else if (ev.key === "ArrowRight") {
      ev.preventDefault();
      video.currentTime = Math.min(duration(), video.currentTime + (ev.shiftKey ? 5 : 1));
      updatePlayhead();
      applyPreview();
      renderRect();
    }
  });
}

function togglePlay() {
  if (!state.probe) return;
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
  $("#btn-delete").addEventListener("click", () => {
    const z = selected();
    if (!z) return;
    state.zooms = state.zooms.filter((x) => x.id !== z.id);
    state.selectedId = null;
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
    requestAnimationFrame(tick);
    $("#btn-play").textContent = "Pause";
  });
  video.addEventListener("pause", () => {
    $("#btn-play").textContent = "Play";
    applyPreview();
    renderRect();
  });
  video.addEventListener("timeupdate", () => {
    if (video.paused) {
      updatePlayhead();
      applyPreview();
      renderRect();
    }
  });
  video.addEventListener("click", togglePlay);

  bindOverlay();
  bindTimeline();
  bindKeys();
  window.addEventListener("pointermove", onPointerMove);
  window.addEventListener("pointerup", onPointerUp);
  window.addEventListener("resize", layoutViewport);
  new ResizeObserver(layoutViewport).observe(stage);
}

main().catch((err) => {
  console.error(err);
  alert(err.message);
});
