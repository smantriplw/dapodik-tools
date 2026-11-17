(() => {
  "use strict";
  const S = window.SMANTI;
  if (!S) return;

  const $ = S.$;

  document.addEventListener("DOMContentLoaded", () => {
    const form = $("#nipd-form");
    if (!form) return;
    const hasScopes = form.dataset.hasScopes === "1";
    let scopeReady = false;

    const els = {
      scopeID: $("#scope-id"),
      scopeSummary: $("#scope-summary"),
      scopeCookieWarning: $("#scope-cookie-warning"),
      modeSwitch: $("#mode-switch"),
      modeHelp: $("#mode-help"),
      renumberOptions: $("#renumber-options"),
      renumberOperator: $("#renumber-operator"),
      renumberThreshold: $("#renumber-threshold"),
      renumberStart: $("#renumber-start"),
      renumberRuleHelp: $("#renumber-rule-help"),
      fillAdvanced: $("#fill-advanced"),
      limit: $("#limit"),
      delay: $("#delay"),
      startNIPD: $("#start-nipd"),
      previewBtn: $("#preview-btn"),
      formError: $("#form-error"),
      previewPanel: $("#preview-panel"),
      progressPanel: $("#progress-panel"),
      progress: $("#job-progress"),
      progressLabel: $("#progress-label"),
      log: $("#job-log"),
      jobLink: $("#job-link"),
      groups: $("#groups"),
      selectAll: $("#select-all"),
      selectedSummary: $("#selected-summary"),
      itemsBody: $("#plan-items"),
      itemsSummary: $("#items-summary"),
      itemFilter: $("#item-filter"),
      recalcBtn: $("#recalc-btn"),
      applyBtn: $("#apply-btn"),
      emptyNotice: $("#empty-notice"),
      renumberNotice: $("#renumber-notice"),
      statTotal: $("#stat-total"),
      statEmpty: $("#stat-empty"),
      statEmptyLabel: $("#stat-empty-label"),
      statHighest: $("#stat-highest"),
      statStart: $("#stat-start"),
      resultActions: $("#result-actions"),
      downloadCSV: $("#download-csv"),
      downloadRollback: $("#download-rollback"),
      downloadJSON: $("#download-json"),
      newPreviewBtn: $("#new-preview-btn"),
      dialog: $("#apply-dialog"),
      applySummary: $("#apply-summary"),
      applyConfirm: $("#apply-confirm"),
      applyConfirmBtn: $("#apply-confirm-btn"),
      applyCancel: $("#apply-cancel"),
    };

    const state = {
      mode: "fill",
      previewMode: null,
      previewJobID: null,
      data: null,
      selected: new Set(),
      recalcSeq: 0,
      busy: false,
      jobID: null,
    };

    let recalcTimer = null;

    function showError(message) {
      els.formError.textContent = message;
      els.formError.hidden = false;
    }
    function clearError() {
      els.formError.hidden = true;
      els.formError.textContent = "";
    }
    function numberOr(input, fallback) {
      const n = Number(input.value);
      return Number.isFinite(n) ? n : fallback;
    }
    function selectedGroups() {
      return Array.from(state.selected);
    }
    function isRenumber(data) {
      return !!data && data.summary && data.summary.mode === "renumber";
    }
    function groupCount(group, renumber) {
      return renumber ? (group.eligible || 0) : (group.empty || 0);
    }
    function updateRenumberRuleHelp() {
      const phrases = {
        ">": "lebih besar dari",
        ">=": "lebih besar atau sama dengan",
        "<": "lebih kecil dari",
        "<=": "lebih kecil atau sama dengan",
      };
      const phrase = phrases[els.renumberOperator.value] || "dibandingkan dengan";
      els.renumberRuleHelp.textContent =
        `Hanya NIPD numerik ${phrase} batas yang diubah. NIPD kosong/non-numerik tidak diubah.`;
    }
    function setBusy(busy, label) {
      state.busy = busy;
      els.previewBtn.disabled = busy || !scopeReady;
      els.previewBtn.textContent = busy ? (label || "Memproses...") : "Muat data & preview";
    }

    function updateScopeSummary() {
      const select = els.scopeID;
      if (!select || !select.options.length) {
        scopeReady = false;
        els.previewBtn.disabled = true;
        if (els.scopeSummary) els.scopeSummary.textContent = "";
        if (els.scopeCookieWarning) els.scopeCookieWarning.hidden = true;
        return;
      }
      const opt = select.options[select.selectedIndex];
      const base = opt.dataset.base || "";
      const sekolah = opt.dataset.sekolah || "";
      const npsn = opt.dataset.npsn || "";
      if (els.scopeSummary) {
        els.scopeSummary.textContent = `${base} · ${sekolah}${npsn ? " · NPSN " + npsn : ""}`;
      }
      const hasCookie = opt.dataset.cookie === "1";
      if (els.scopeCookieWarning) els.scopeCookieWarning.hidden = hasCookie;
      scopeReady = hasCookie;
      els.previewBtn.disabled = state.busy || !scopeReady;
    }

    function resetPreview() {
      state.previewJobID = null;
      state.previewMode = null;
      state.data = null;
      state.selected.clear();
      state.jobID = null;
      els.previewPanel.hidden = true;
      els.progressPanel.hidden = true;
      els.resultActions.hidden = true;
      els.downloadRollback.hidden = true;
      els.groups.innerHTML = "";
      els.itemsBody.innerHTML = "";
      els.itemFilter.value = "";
      els.emptyNotice.hidden = true;
      els.renumberNotice.hidden = true;
      els.applyBtn.disabled = true;
    }

    function setMode(mode) {
      if (state.busy) return;
      state.mode = mode === "renumber" ? "renumber" : "fill";
      for (const btn of els.modeSwitch.querySelectorAll("[data-mode]")) {
        const active = btn.dataset.mode === state.mode;
        btn.classList.toggle("is-active", active);
        btn.setAttribute("aria-selected", active ? "true" : "false");
      }
      const renumber = state.mode === "renumber";
      els.renumberOptions.hidden = !renumber;
      els.fillAdvanced.hidden = renumber;
      els.statEmptyLabel.textContent = renumber ? "Akan diubah" : "NIPD kosong";
      els.modeHelp.textContent = renumber
        ? "Renumber menimpa NIPD lama sesuai operator pembanding yang dipilih."
        : "Isi hanya siswa yang belum punya NIPD. NIPD lama tidak diubah.";
      updateRenumberRuleHelp();
      clearError();
      resetPreview();
    }

    function validateMode() {
      if (state.mode !== "renumber") return true;
      const rawThreshold = els.renumberThreshold.value.trim();
      const rawStart = els.renumberStart.value.trim();
      const threshold = Number(rawThreshold);
      const start = Number(rawStart);
      if (!rawThreshold || !Number.isInteger(threshold) || threshold < 0) {
        showError("Batas NIPD wajib diisi dengan angka bulat 0 atau lebih.");
        return false;
      }
      if (!rawStart || !Number.isInteger(start) || start < 0) {
        showError("NIPD awal baru wajib diisi dengan angka bulat 0 atau lebih.");
        return false;
      }
      return true;
    }

    function jobPayload() {
      const payload = {
        scope_id: els.scopeID ? els.scopeID.value : "",
        limit: numberOr(els.limit, 20000),
        delay: numberOr(els.delay, 0.3),
      };
      if (state.mode === "renumber") {
        payload.threshold = Number(els.renumberThreshold.value);
        payload.operator = els.renumberOperator.value;
        payload.start_nipd = Number(els.renumberStart.value);
      } else {
        const start = numberOr(els.startNIPD, 0);
        if (start > 0) payload.start_nipd = start;
      }
      return payload;
    }

    function validateCredentials() {
      if (!hasScopes) {
        showError("Belum ada scope Dapodik yang tersedia untuk akun ini.");
        return false;
      }
      if (!scopeReady) {
        showError("Scope yang dipilih belum memiliki cookie. Hubungi admin.");
        return false;
      }
      return true;
    }

    function setStat(el, value) {
      if (el) el.textContent = String(value);
    }

    function renderStats(data) {
      const renumber = isRenumber(data);
      const s = data.summary || {};
      const count = renumber ? (s.eligible ?? 0) : (s.empty ?? 0);
      const anyEligible = (data.groups || []).some((g) => groupCount(g, renumber) > 0);
      setStat(els.statTotal, s.total_students ?? 0);
      setStat(els.statEmpty, count);
      setStat(els.statHighest, s.highest_nipd ?? 0);
      setStat(els.statStart, s.start_nipd ?? 0);
      els.statEmptyLabel.textContent = renumber ? "Akan diubah" : "NIPD kosong";

      if (renumber) {
        els.emptyNotice.hidden = true;
        els.renumberNotice.hidden = false;
        if ((s.conflict_count ?? 0) > 0) {
          const list = (s.conflicts || []).slice(0, 8).join(", ");
          const suffix = (s.conflicts || []).length > 8 ? ", …" : "";
          els.renumberNotice.textContent =
            `Konflik: ${s.conflict_count} NIPD non-target berada di rentang ${s.new_min}–${s.new_max} (${list}${suffix}). Apply diblokir.`;
        } else if (count === 0) {
          els.renumberNotice.textContent = anyEligible
            ? "Pilih minimal satu rombel yang memiliki siswa eligible."
            : `Tidak ada siswa dengan NIPD ${s.operator || ">"} ${s.threshold ?? 0}. Tidak ada yang perlu direnumber.`;
        } else {
          els.renumberNotice.textContent =
            `Renumber ${count} siswa dengan NIPD ${s.operator || ">"} ${s.threshold ?? 0} → NIPD ${s.start_nipd}–${s.new_max}. ` +
            `${s.kept ?? 0} siswa dipertahankan, ${s.skipped ?? 0} dilewati karena registrasi_id kosong.`;
        }
      } else {
        els.renumberNotice.hidden = true;
        els.emptyNotice.hidden = count !== 0;
      }
    }

    function renderGroups(data, renumber) {
      els.groups.innerHTML = "";
      const groups = data.groups || [];
      for (const group of groups) {
        const count = groupCount(group, renumber);
        const row = document.createElement("label");
        row.className = "group-row";
        if (count === 0) row.classList.add("group-row-disabled");
        const cb = document.createElement("input");
        cb.type = "checkbox";
        cb.value = group.label;
        cb.checked = count > 0;
        cb.disabled = count === 0;
        cb.addEventListener("change", () => {
          if (cb.checked) state.selected.add(group.label);
          else state.selected.delete(group.label);
          updateSelectionSummary();
          scheduleRecalc();
        });
        const label = document.createElement("span");
        label.className = "group-name";
        label.textContent = group.label;
        const meta = document.createElement("span");
        meta.className = "muted small";
        meta.textContent = `${group.total} siswa, ${count} ${renumber ? "akan diubah" : "kosong"}`;
        row.append(cb, label, meta);
        els.groups.appendChild(row);
        if (cb.checked) state.selected.add(group.label);
      }
      els.selectAll.checked = groups.length > 0 && groups.every((g) => groupCount(g, renumber) === 0 || state.selected.has(g.label));
      els.selectAll.disabled = groups.every((g) => groupCount(g, renumber) === 0);
      updateSelectionSummary();
    }

    function updateSelectionSummary() {
      const data = state.data;
      if (!data) return;
      const renumber = isRenumber(data);
      let count = 0;
      for (const group of data.groups || []) {
        if (state.selected.has(group.label)) count += groupCount(group, renumber);
      }
      els.selectedSummary.textContent =
        `${state.selected.size} rombel dipilih, ${count} ${renumber ? "siswa akan di-renumber" : "NIPD kosong"}`;
      const conflict = renumber && ((data.summary && data.summary.conflict_count) || 0) > 0;
      els.applyBtn.disabled = state.selected.size === 0 || count === 0 || state.busy || conflict;
    }

    function filteredItems() {
      const q = els.itemFilter.value.trim().toLowerCase();
      const items = (state.data && state.data.items) || [];
      if (!q) return items;
      return items.filter((it) =>
        (it.nama || "").toLowerCase().includes(q) || (it.nisn || "").includes(q)
      );
    }

    function renderItems() {
      const renumber = isRenumber(state.data);
      const items = filteredItems();
      els.itemsBody.innerHTML = "";
      for (const it of items) {
        const tr = document.createElement("tr");
        const values = [it.group, it.nama, it.nisn, it.old_nipd || "\u2014", it.nipd];
        for (const value of values) {
          const td = document.createElement("td");
          td.textContent = value;
          if (value === it.nisn || value === it.nipd) td.className = "mono";
          if (value === "\u2014") td.className = "mono muted";
          if (it.group === "(Tanpa Rombel)" && value === it.group) {
            const badge = document.createElement("span");
            badge.className = "badge badge-warning";
            badge.textContent = it.group;
            td.textContent = "";
            td.appendChild(badge);
          }
          tr.appendChild(td);
        }
        els.itemsBody.appendChild(tr);
      }
      const total = (state.data && state.data.items || []).length;
      const shown = items.length;
      els.itemsSummary.textContent = shown === total
        ? `${total} siswa akan ${renumber ? "di-renumber" : "diisi"}`
        : `Menampilkan ${shown} dari ${total} siswa`;
    }

    function scheduleRecalc() {
      clearTimeout(recalcTimer);
      recalcTimer = setTimeout(() => recalc(), 250);
    }

    async function recalc() {
      if (!state.previewJobID) return;
      const seq = ++state.recalcSeq;
      const renumberMode = state.previewMode === "renumber";
      els.recalcBtn.disabled = true;
      els.applyBtn.disabled = true;
      try {
        const endpoint = renumberMode ? "/nipd-filler/api/renumber/recalc" : "/nipd-filler/api/recalc";
        const data = await S.api(endpoint, {
          method: "POST",
          json: {
            job_id: state.previewJobID,
            groups: selectedGroups(),
          },
        });
        if (seq !== state.recalcSeq) return;
        state.data = data;
        renderStats(data);
        renderItems();
        updateSelectionSummary();
      } catch (err) {
        showError(err.message);
      } finally {
        if (seq === state.recalcSeq) {
          els.recalcBtn.disabled = false;
          updateSelectionSummary();
        }
      }
    }

    async function loadPreview(jobID) {
      const mode = state.previewMode || state.mode;
      const renumber = mode === "renumber";
      const endpoint = renumber
        ? `/nipd-filler/api/renumber/preview/${encodeURIComponent(jobID)}`
        : `/nipd-filler/api/preview/${encodeURIComponent(jobID)}`;
      const data = await S.api(endpoint);
      state.previewJobID = jobID;
      state.data = data;
      const key = renumber ? "eligible" : "empty";
      state.selected = new Set((data.groups || []).filter((g) => (g[key] || 0) > 0).map((g) => g.label));
      els.previewPanel.hidden = false;
      renderStats(data);
      renderGroups(data, renumber);
      renderItems();
      updateSelectionSummary();
      els.previewPanel.scrollIntoView({ behavior: "smooth", block: "start" });
    }

    function handleJobEnd(jobID, kind, status) {
      els.progressLabel.textContent = status === "done" ? "Selesai" : "Job berhenti: " + status;
      if (kind === "preview" && status === "done") {
        loadPreview(jobID).catch((err) => showError(err.message));
      }
      if (kind === "apply") {
        finishApply(jobID, status);
      }
    }

    async function finishApply(jobID, status) {
      let snap = null;
      try {
        snap = await S.api(`/jobs/${encodeURIComponent(jobID)}/snapshot`);
      } catch (err) { /* ignore */ }
      if (status === "done" && snap?.counters) {
        els.progressLabel.textContent =
          `Selesai: ${snap.counters.done} berhasil, ${snap.counters.failed} gagal dari ${snap.counters.total} item.`;
      } else if (snap?.job?.error) {
        els.progressLabel.textContent = "Job gagal: " + snap.job.error;
      }
      els.downloadCSV.href = `/jobs/${encodeURIComponent(jobID)}/download.csv`;
      els.downloadJSON.href = `/jobs/${encodeURIComponent(jobID)}/download.json`;
      if (state.previewMode === "renumber") {
        els.downloadRollback.href = `/jobs/${encodeURIComponent(jobID)}/download.rollback.csv`;
        els.downloadRollback.hidden = false;
      } else {
        els.downloadRollback.hidden = true;
      }
      els.resultActions.hidden = false;
      setBusy(false);
      els.applyBtn.disabled = false;
    }

    function startJob(jobID, kind) {
      state.jobID = jobID;
      els.progressPanel.hidden = false;
      els.resultActions.hidden = true;
      els.jobLink.href = `/jobs/${encodeURIComponent(jobID)}`;
      els.log.textContent = "";
      S.setProgress(els.progress, els.progressLabel, 0, 1, "Menyiapkan job...");
      els.progressPanel.scrollIntoView({ behavior: "smooth", block: "start" });
      S.streamJob(jobID, {
        onEvent(ev) {
          S.appendLog(els.log, ev);
          S.applyEventProgress(ev, els.progress, els.progressLabel);
        },
        onSnapshot(snap) {
          if (snap?.counters) {
            S.setProgress(
              els.progress, els.progressLabel,
              snap.counters.current, snap.counters.total,
              `${snap.counters.current}/${snap.counters.total}`
            );
          }
        },
        onEnd(payload) {
          handleJobEnd(jobID, kind, (payload && payload.status) || "done");
        },
      });
    }

    for (const btn of els.modeSwitch.querySelectorAll("[data-mode]")) {
      btn.addEventListener("click", () => setMode(btn.dataset.mode));
    }

    if (els.scopeID) {
      els.scopeID.addEventListener("change", updateScopeSummary);
    }
    if (els.renumberOperator) {
      els.renumberOperator.addEventListener("change", updateRenumberRuleHelp);
    }
    setMode("fill");
    updateScopeSummary();

    form.addEventListener("submit", async (event) => {
      event.preventDefault();
      clearError();
      if (!validateCredentials() || !validateMode()) return;
      setBusy(true, "Mengambil data...");
      els.previewPanel.hidden = true;
      els.progressPanel.hidden = true;
      try {
        const endpoint = state.mode === "renumber"
          ? "/nipd-filler/api/renumber/preview"
          : "/nipd-filler/api/preview";
        state.previewMode = state.mode;
        const res = await S.api(endpoint, { method: "POST", json: jobPayload() });
        startJob(res.job_id, "preview");
      } catch (err) {
        showError(err.message);
      } finally {
        setBusy(false);
      }
    });

    els.selectAll.addEventListener("change", () => {
      const data = state.data;
      if (!data) return;
      const renumber = isRenumber(data);
      const check = els.selectAll.checked;
      state.selected.clear();
      for (const group of data.groups || []) {
        if (groupCount(group, renumber) === 0) continue;
        if (check) state.selected.add(group.label);
      }
      for (const cb of Array.from(els.groups.querySelectorAll('input[type="checkbox"]'))) {
        if (!cb.disabled) cb.checked = check;
      }
      updateSelectionSummary();
      scheduleRecalc();
    });

    els.recalcBtn.addEventListener("click", () => recalc());

    els.itemFilter.addEventListener("input", () => renderItems());

    els.applyBtn.addEventListener("click", () => {
      if (!state.data || (state.data.items || []).length === 0) return;
      const items = state.data.items;
      const first = items[0];
      const last = items[items.length - 1];
      const s = state.data.summary || {};
      if (isRenumber(state.data)) {
        els.applySummary.textContent =
          `${items.length} siswa dari ${selectedGroups().length} rombel akan di-renumber. ` +
          `Batas ${s.operator || ">"} ${s.threshold}, NIPD ${first.nipd} sampai ${last.nipd}.`;
      } else {
        els.applySummary.textContent =
          `${items.length} siswa dari ${selectedGroups().length} rombel. ` +
          `NIPD ${first.nipd} sampai ${last.nipd}.`;
      }
      els.applyConfirm.checked = false;
      els.applyConfirmBtn.disabled = true;
      els.dialog.showModal();
    });

    els.applyConfirm.addEventListener("change", () => {
      els.applyConfirmBtn.disabled = !els.applyConfirm.checked;
    });
    els.applyCancel.addEventListener("click", () => els.dialog.close());

    els.applyConfirmBtn.addEventListener("click", async () => {
      if (!validateCredentials()) {
        els.dialog.close();
        return;
      }
      if (!state.previewJobID) return;
      const renumberMode = state.previewMode === "renumber";
      els.applyConfirmBtn.disabled = true;
      els.dialog.close();
      setBusy(true, "Mengirim apply...");
      try {
        const endpoint = renumberMode
          ? "/nipd-filler/api/renumber/apply"
          : "/nipd-filler/api/apply";
        const res = await S.api(endpoint, {
          method: "POST",
          json: {
            preview_job_id: state.previewJobID,
            groups: selectedGroups(),
            delay: numberOr(els.delay, 0.3),
          },
        });
        startJob(res.job_id, "apply");
      } catch (err) {
        showError(err.message);
      } finally {
        setBusy(false);
      }
    });

    els.newPreviewBtn.addEventListener("click", () => {
      resetPreview();
      window.scrollTo({ top: 0, behavior: "smooth" });
    });
  });
})();
