(() => {
  "use strict";
  const S = window.SMANTI;
  if (!S) return;
  const $ = S.$;

  document.addEventListener("DOMContentLoaded", () => {
    const form = $("#lookup-form");
    if (!form) return;
    const hasScopes = form.dataset.hasScopes === "1";
    let scopeReady = false;

    const els = {
      nisn: $("#nisn"),
      scopeID: $("#scope-id"),
      scopeSummary: $("#scope-summary"),
      scopeCookieWarning: $("#scope-cookie-warning"),
      limit: $("#limit"),
      button: $("#lookup-btn"),
      error: $("#lookup-error"),
      panel: $("#result-panel"),
      meta: $("#result-meta"),
      missing: $("#missing-box"),
      rows: $("#result-rows"),
    };

    function updateScopeSummary() {
      const select = els.scopeID;
      if (!select || !select.options.length) {
        scopeReady = false;
        els.button.disabled = true;
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
      els.button.disabled = !scopeReady;
    }

    function showError(message) {
      els.error.textContent = message;
      els.error.hidden = false;
    }
    function clearError() {
      els.error.hidden = true;
      els.error.textContent = "";
    }

    function cell(text, className) {
      const td = document.createElement("td");
      td.textContent = text;
      if (className) td.className = className;
      return td;
    }

    function render(data) {
      els.rows.innerHTML = "";
      for (const row of data.results || []) {
        const tr = document.createElement("tr");
        tr.appendChild(cell(row.nama || "-"));
        tr.appendChild(cell(row.nisn || "-", "mono"));
        const rombelTd = document.createElement("td");
        if (row.has_rombel) {
          rombelTd.textContent = row.rombel;
        } else {
          const badge = document.createElement("span");
          badge.className = "badge badge-warning";
          badge.textContent = "(Tanpa Rombel)";
          rombelTd.appendChild(badge);
        }
        tr.appendChild(rombelTd);
        tr.appendChild(cell(row.tingkat || "-"));
        const nipdTd = document.createElement("td");
        if (row.nipd) {
          nipdTd.textContent = row.nipd;
          nipdTd.className = "mono";
        } else {
          const badge = document.createElement("span");
          badge.className = "badge badge-danger";
          badge.textContent = "Kosong";
          nipdTd.appendChild(badge);
        }
        tr.appendChild(nipdTd);
        els.rows.appendChild(tr);
      }

      if ((data.missing || []).length > 0) {
        els.missing.hidden = false;
        els.missing.textContent = "NISN tidak ditemukan: " + data.missing.join(", ");
      } else {
        els.missing.hidden = true;
        els.missing.textContent = "";
      }

      const cache = data.from_cache ? "cache memori" : "baru";
      els.meta.textContent = `${data.total_students} siswa dimuat (${cache})`;
      els.panel.hidden = false;
      els.panel.scrollIntoView({ behavior: "smooth", block: "start" });
    }

    if (els.scopeID) {
      els.scopeID.addEventListener("change", updateScopeSummary);
    }
    updateScopeSummary();

    form.addEventListener("submit", async (event) => {
      event.preventDefault();
      clearError();
      if (!hasScopes) {
        showError("Belum ada scope Dapodik yang tersedia untuk akun ini.");
        return;
      }
      if (!scopeReady) {
        showError("Scope yang dipilih belum memiliki cookie. Hubungi admin.");
        return;
      }
      els.button.disabled = true;
      els.button.textContent = "Mencari...";
      try {
        const data = await S.api("/student-lookup/api/search", {
          method: "POST",
          json: {
            scope_id: els.scopeID ? els.scopeID.value : "",
            nisn: els.nisn.value,
            limit: Number(els.limit.value) || 20000,
          },
        });
        render(data);
      } catch (err) {
        showError(err.message);
      } finally {
        els.button.disabled = false;
        els.button.textContent = "Cari siswa";
      }
    });
  });
})();
