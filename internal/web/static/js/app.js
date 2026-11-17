(() => {
  "use strict";

  const csrf = document.querySelector('meta[name="csrf-token"]')?.content || "";
  const $ = (sel, root = document) => root.querySelector(sel);
  const $$ = (sel, root = document) => Array.from(root.querySelectorAll(sel));

  async function api(url, options = {}) {
    const opts = { ...options };
    const headers = new Headers(opts.headers || {});
    headers.set("Accept", "application/json");
    if ((opts.method || "GET").toUpperCase() !== "GET") {
      headers.set("X-CSRF-Token", csrf);
    }
    let body = opts.body;
    if (opts.json !== undefined) {
      headers.set("Content-Type", "application/json");
      body = JSON.stringify(opts.json);
      delete opts.json;
    }
    const res = await fetch(url, { ...opts, headers, body });
    const text = await res.text();
    let data = null;
    if (text) {
      try { data = JSON.parse(text); } catch { data = null; }
    }
    if (!res.ok) {
      const message = (data && data.error) || (text ? text.slice(0, 240) : `HTTP ${res.status}`);
      throw new Error(message);
    }
    return data;
  }

  function fmtClock(ts) {
    if (!ts) return "";
    const d = new Date(ts);
    if (Number.isNaN(d.getTime())) return ts;
    return d.toLocaleTimeString("id-ID", { hour12: false, timeZone: "Asia/Makassar" });
  }

  function levelClass(level) {
    switch ((level || "").toLowerCase()) {
      case "error": return "log-error";
      case "warn": return "log-warn";
      case "success": return "log-success";
      default: return "";
    }
  }

  function appendLog(container, ev) {
    if (!container || !ev) return;
    const line = document.createElement("span");
    line.className = "log-line " + levelClass(ev.level);
    const time = fmtClock(ev.ts) || "--:--:--";
    line.textContent = `[${time}] [${(ev.level || "info").toUpperCase()}] ${ev.message || ""}\n`;
    container.appendChild(line);
    container.scrollTop = container.scrollHeight;
  }

  function setProgress(el, labelEl, current, total, label) {
    if (!el) return;
    const max = total > 0 ? total : Math.max(current || 0, 1);
    el.max = max;
    el.value = Math.max(0, Math.min(current || 0, max));
    if (labelEl) {
      labelEl.textContent = label || `${current || 0}/${total || 0}`;
    }
  }

  function applyEventProgress(ev, progressEl, labelEl) {
    if (!ev || !ev.data) return;
    const level = (ev.level || "").toLowerCase();
    if (level !== "progress") return;
    const data = ev.data;
    if (typeof data.current === "number" && typeof data.total === "number") {
      setProgress(progressEl, labelEl, data.current, data.total, data.label || ev.message);
    }
  }

  function streamJob(jobId, handlers = {}, after = 0) {
    const suffix = after > 0 ? `?after=${encodeURIComponent(after)}` : "";
    const url = `/jobs/${encodeURIComponent(jobId)}/events${suffix}`;
    let lastSeq = after;
    let finished = false;
    let polling = false;

    const finish = (payload) => {
      if (finished) return;
      finished = true;
      if (handlers.onEnd) handlers.onEnd(payload);
    };

    const handleEvent = (ev) => {
      if (!ev) return;
      if (ev.seq) lastSeq = Math.max(lastSeq, ev.seq);
      if (handlers.onEvent) handlers.onEvent(ev);
    };

    const poll = async () => {
      if (finished || polling) return;
      polling = true;
      try {
        const snap = await api(`/jobs/${encodeURIComponent(jobId)}/snapshot?after=${lastSeq}`);
        if (snap && Array.isArray(snap.events)) {
          for (const ev of snap.events) handleEvent(ev);
        }
        if (handlers.onSnapshot) handlers.onSnapshot(snap);
        polling = false;
        if (snap && snap.job && ["done", "failed", "cancelled"].includes(snap.job.status)) {
          finish(snap);
          return;
        }
      } catch (err) {
        polling = false;
        if (err && /login|akses|csrf|sesi/i.test(err.message || "")) {
          finish({ status: "failed", error: err.message });
          return;
        }
      }
      if (!finished) setTimeout(poll, 1500);
    };

    let es;
    try {
      es = new EventSource(url);
    } catch (err) {
      poll();
      return { close: () => {} };
    }
    es.addEventListener("message", (event) => {
      try { handleEvent(JSON.parse(event.data)); } catch { /* ignore */ }
    });
    es.addEventListener("end", (event) => {
      try { finish(JSON.parse(event.data || "{}")); } catch { finish({}); }
      es.close();
    });
    es.onerror = () => {
      if (finished) return;
      es.close();
      poll();
    };
    return {
      close: () => {
        finished = true;
        es.close();
      },
    };
  }

  function initSidebar() {
    $$("[data-sidebar-toggle]").forEach((btn) => {
      btn.addEventListener("click", () => document.body.classList.toggle("sidebar-open"));
    });
    $$("[data-sidebar-close]").forEach((btn) => {
      btn.addEventListener("click", () => document.body.classList.remove("sidebar-open"));
    });
  }

  function initScopeTest() {
    const btn = $("#scope-test");
    if (!btn) return;
    const scopeID = btn.dataset.scopeId || "";
    const box = $("#scope-test-result");
    btn.addEventListener("click", async () => {
      btn.disabled = true;
      const previous = btn.textContent;
      btn.textContent = "Menguji...";
      if (box) {
        box.hidden = true;
        box.textContent = "";
        box.className = "notice";
      }
      try {
        const data = await api(`/admin/scopes/${encodeURIComponent(scopeID)}/test`, { method: "POST" });
        if (box) {
          box.hidden = false;
          box.className = "notice";
          box.textContent = (data && data.message) || "Koneksi berhasil.";
        }
      } catch (err) {
        if (box) {
          box.hidden = false;
          box.className = "notice notice-warning";
          box.textContent = "Gagal: " + err.message;
        }
      } finally {
        btn.disabled = false;
        btn.textContent = previous;
      }
    });
  }

  function initConfirmForms() {
    document.addEventListener("submit", (event) => {
      const form = event.target.closest("form[data-confirm]");
      if (!form) return;
      if (!window.confirm(form.dataset.confirm || "Lanjutkan?")) {
        event.preventDefault();
      }
    });
  }

  function initFlash() {
    document.addEventListener("click", (event) => {
      const close = event.target.closest("[data-dismiss]");
      if (close) {
        const flash = close.closest(".flash");
        if (flash) flash.remove();
      }
    });
  }

  function initJobPage() {
    const root = $("#job-detail");
    if (!root) return;
    const jobId = root.dataset.jobId;
    const initialStatus = (root.dataset.status || "").toLowerCase();
    const afterSeq = Number(root.dataset.afterSeq || 0);
    const logEl = $("#job-log");
    const progressEl = $("#job-progress");
    const labelEl = $("#job-progress-label");
    const statusBadge = $("#job-status-badge");
    const counters = {
      ok: $("#counter-ok"),
      failed: $("#counter-failed"),
      total: $("#counter-total"),
    };

    streamJob(jobId, {
      onEvent(ev) {
        appendLog(logEl, ev);
        applyEventProgress(ev, progressEl, labelEl);
      },
      onSnapshot(snap) {
        if (!snap) return;
        if (snap.counters) {
          if (counters.ok) counters.ok.textContent = snap.counters.done;
          if (counters.failed) counters.failed.textContent = snap.counters.failed;
          if (counters.total) counters.total.textContent = snap.counters.total;
          setProgress(progressEl, labelEl, snap.counters.current, snap.counters.total, null);
        }
        if (snap.job && statusBadge) {
          statusBadge.textContent = statusLabel(snap.job.status);
          statusBadge.className = "badge " + statusClass(snap.job.status);
        }
        if (snap.items && $("#job-items") && snap.items.length) {
          renderReferenceItems(snap.items);
        }
      },
      onEnd() {
        if (!["done", "failed", "cancelled"].includes(initialStatus)) {
          setTimeout(() => window.location.reload(), 700);
        }
      },
    }, afterSeq);

    const cancelBtn = $("#cancel-job");
    if (cancelBtn) {
      cancelBtn.addEventListener("click", async () => {
        if (!window.confirm("Batalkan job ini?")) return;
        cancelBtn.disabled = true;
        cancelBtn.textContent = "Membatalkan...";
        try {
          await api(`/jobs/${encodeURIComponent(jobId)}/cancel`, { method: "POST" });
        } catch (err) {
          window.alert(err.message);
          cancelBtn.disabled = false;
          cancelBtn.textContent = "Batalkan";
        }
      });
    }
  }

  function renderReferenceItems(items) {
    const tbody = $("#job-items");
    if (!tbody) return;
    tbody.innerHTML = "";
    for (const it of items) {
      const tr = document.createElement("tr");
      const cells = [
        it.group === "(Tanpa Rombel)" ? it.group : it.group,
        it.nama,
        it.nisn,
        it.old_nipd || "\u2014",
        it.nipd,
      ];
      for (const value of cells) {
        const td = document.createElement("td");
        td.textContent = value;
        tr.appendChild(td);
      }
      const td = document.createElement("td");
      const badge = document.createElement("span");
      badge.className = "badge " + statusClass(it.status);
      badge.textContent = statusLabel(it.status);
      td.appendChild(badge);
      tr.appendChild(td);
      tbody.appendChild(tr);
    }
  }

  function statusClass(status) {
    switch ((status || "").toLowerCase()) {
      case "done": case "ok": case "success": return "badge-success";
      case "running": return "badge-running";
      case "queued": case "pending": case "dry-run": return "badge-pending";
      case "failed": case "error": return "badge-danger";
      default: return "badge-muted";
    }
  }

  function statusLabel(status) {
    switch ((status || "").toLowerCase()) {
      case "done": case "success": return "Selesai";
      case "running": return "Berjalan";
      case "queued": return "Menunggu";
      case "pending": return "Pending";
      case "failed": case "error": return "Gagal";
      case "cancelled": return "Dibatalkan";
      case "ok": return "Berhasil";
      case "skipped": return "Dilewati";
      case "dry-run": return "Dry-run";
      default: return status || "-";
    }
  }

  document.addEventListener("DOMContentLoaded", () => {
    initSidebar();
    initFlash();
    initScopeTest();
    initConfirmForms();
    initJobPage();
  });

  window.SMANTI = {
    csrf, $, $$, api, streamJob, appendLog, setProgress, applyEventProgress,
    statusClass, statusLabel, fmtClock,
  };
})();
