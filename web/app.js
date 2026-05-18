(() => {
  const REFRESH_MS = 2000;
  const SPARK_W = 200;
  const SPARK_H = 60;

  const els = {
    statusBox: document.getElementById("status"),
    statusText: document.getElementById("status-text"),
    proberInfo: document.getElementById("prober-info"),
    direct: {
      column: document.querySelector(".column--internet"),
      current: document.getElementById("direct-current"),
      p50: document.getElementById("direct-p50"),
      p95: document.getElementById("direct-p95"),
      ttfb: document.getElementById("direct-ttfb"),
      loss: document.getElementById("direct-loss"),
      errors: document.getElementById("direct-errors"),
      count: document.getElementById("direct-count"),
      target: document.getElementById("direct-target"),
      spark: document.querySelector("#direct-spark .spark__line"),
    },
    datum: {
      column: document.querySelector(".column--datum"),
      current: document.getElementById("datum-current"),
      p50: document.getElementById("datum-p50"),
      p95: document.getElementById("datum-p95"),
      ttfb: document.getElementById("datum-ttfb"),
      loss: document.getElementById("datum-loss"),
      errors: document.getElementById("datum-errors"),
      count: document.getElementById("datum-count"),
      target: document.getElementById("datum-target"),
      spark: document.querySelector("#datum-spark .spark__line"),
      banner: document.getElementById("datum-banner"),
    },
  };

  function fmtMs(v) {
    if (!Number.isFinite(v) || v <= 0) return "—";
    if (v < 10) return v.toFixed(1);
    return Math.round(v).toString();
  }

  function fmtPct(v) {
    if (!Number.isFinite(v)) return "—";
    return v.toFixed(v < 10 ? 1 : 0) + "%";
  }

  function renderSpark(line, samples) {
    if (!samples || samples.length === 0) {
      line.setAttribute("points", "");
      return;
    }
    const values = samples.map((s) => (s.ok ? s.total_ms : null));
    const finite = values.filter((v) => v != null);
    if (finite.length === 0) {
      line.setAttribute("points", "");
      return;
    }
    const max = Math.max(...finite, 1);
    const min = Math.min(...finite, 0);
    const span = Math.max(max - min, 1);
    const n = samples.length;
    const points = values
      .map((v, i) => {
        if (v == null) return null;
        const x = n === 1 ? SPARK_W : (i / (n - 1)) * SPARK_W;
        const y = SPARK_H - ((v - min) / span) * (SPARK_H - 4) - 2;
        return `${x.toFixed(1)},${y.toFixed(1)}`;
      })
      .filter((p) => p != null)
      .join(" ");
    line.setAttribute("points", points);
  }

  function renderPath(refs, summary) {
    refs.current.textContent = fmtMs(summary.current_ms);
    refs.p50.textContent = fmtMs(summary.p50_ms);
    refs.p95.textContent = fmtMs(summary.p95_ms);
    refs.ttfb.textContent = fmtMs(summary.p50_ttfb_ms);
    refs.loss.textContent = fmtPct(summary.retrans_pct);
    refs.errors.textContent = fmtPct(summary.error_pct);
    refs.count.textContent = summary.sample_count.toString();
    renderSpark(refs.spark, summary.samples);
  }

  function setStatus(state, text) {
    els.statusBox.dataset.state = state;
    els.statusText.textContent = text;
  }

  async function tick() {
    try {
      const r = await fetch("/api/metrics", { cache: "no-store" });
      if (!r.ok) throw new Error(`HTTP ${r.status}`);
      const data = await r.json();

      els.direct.target.textContent = data.targets.direct_url || "—";
      els.datum.target.textContent = data.targets.datum_url || "—";

      renderPath(els.direct, data.direct);
      renderPath(els.datum, data.datum);

      if (data.datum_path_down) {
        els.datum.column.classList.add("is-down");
        els.datum.banner.hidden = false;
        setStatus("warn", "Datum tunnel offline");
      } else {
        els.datum.column.classList.remove("is-down");
        els.datum.banner.hidden = true;
        setStatus("ok", `updated ${new Date().toLocaleTimeString()}`);
      }

      if (data.prober_started_at) {
        els.proberInfo.textContent = `prober up since ${new Date(data.prober_started_at).toLocaleTimeString()}`;
      }
    } catch (err) {
      setStatus("bad", `error: ${err.message}`);
    }
  }

  tick();
  setInterval(tick, REFRESH_MS);
})();
