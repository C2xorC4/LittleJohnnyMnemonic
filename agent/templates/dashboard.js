(function () {
  'use strict';

  const DATA = window.__DASHBOARD_DATA__;
  const tip = document.getElementById('tooltip');

  // ── Colour palette ──────────────────────────────────────────────
  const TYPE_COLORS = {
    episodic:  '#e07b54',
    feedback:  '#4ecdc4',
    knowledge: '#45b7d1',
    project:   '#96ceb4',
    reference: '#f0c94a',
    semantic:  '#c084fc',
    user:      '#6ee7b7',
  };
  const FALLBACK_COLORS = ['#8b9dc3','#f7a4a4','#a4c4f7','#d4a4f7','#a4f7c4'];
  let fallbackIdx = 0;
  function typeColor(t) {
    if (!TYPE_COLORS[t]) {
      TYPE_COLORS[t] = FALLBACK_COLORS[fallbackIdx++ % FALLBACK_COLORS.length];
    }
    return TYPE_COLORS[t];
  }

  // ── Chart layout constants ──────────────────────────────────────
  const PAD = { top: 18, right: 16, bottom: 44, left: 46 };

  function ca(canvas) {
    return {
      x: PAD.left,
      y: PAD.top,
      w: canvas.width - PAD.left - PAD.right,
      h: canvas.height - PAD.top - PAD.bottom,
    };
  }

  function niceMax(v) {
    if (v <= 0) return 10;
    const magnitude = Math.pow(10, Math.floor(Math.log10(v)));
    const norm = v / magnitude;
    const step = norm <= 1.5 ? 2 : norm <= 3.5 ? 4 : norm <= 7 ? 8 : 10;
    return step * magnitude * 1.05;
  }

  // ── Axes & grid ─────────────────────────────────────────────────
  function drawAxes(ctx, area, xLabels, yMax) {
    const steps = 4;
    ctx.save();
    ctx.font = '10px monospace';
    ctx.textBaseline = 'middle';

    // Y gridlines + labels
    for (let i = 0; i <= steps; i++) {
      const v = (yMax / steps) * i;
      const y = area.y + area.h - (i / steps) * area.h;

      ctx.beginPath();
      ctx.setLineDash([2, 5]);
      ctx.strokeStyle = 'rgba(255,255,255,0.04)';
      ctx.moveTo(area.x, y);
      ctx.lineTo(area.x + area.w, y);
      ctx.stroke();
      ctx.setLineDash([]);

      ctx.fillStyle = '#6b6b8a';
      ctx.textAlign = 'right';
      const label = v >= 1000 ? (v / 1000).toFixed(1) + 'k' : String(Math.round(v));
      ctx.fillText(label, area.x - 4, y);
    }

    // X labels — thin out to avoid crowding
    const maxLabels = Math.max(2, Math.floor(area.w / 44));
    const step = Math.max(1, Math.ceil(xLabels.length / maxLabels));
    const barW = area.w / xLabels.length;
    ctx.fillStyle = '#6b6b8a';
    ctx.textAlign = 'center';
    ctx.textBaseline = 'top';
    xLabels.forEach((label, i) => {
      if (i % step !== 0) return;
      const x = area.x + (i + 0.5) * barW;
      ctx.fillText(label.slice(5), x, area.y + area.h + 6); // "MM-DD"
    });

    // Axis lines
    ctx.beginPath();
    ctx.strokeStyle = '#2a2a45';
    ctx.lineWidth = 1;
    ctx.moveTo(area.x, area.y);
    ctx.lineTo(area.x, area.y + area.h);
    ctx.lineTo(area.x + area.w, area.y + area.h);
    ctx.stroke();

    ctx.restore();
  }

  // ── Bar chart (single series) ───────────────────────────────────
  function drawBarChart(canvas, points, valueKey, color) {
    if (!points || points.length === 0) { drawEmpty(canvas); return; }
    const ctx = canvas.getContext('2d');
    ctx.clearRect(0, 0, canvas.width, canvas.height);

    const values = points.map(p => p[valueKey] || 0);
    const yMax = niceMax(Math.max(...values));
    const area = ca(canvas);
    const barW = area.w / points.length;

    drawAxes(ctx, area, points.map(p => p.date), yMax);

    values.forEach((v, i) => {
      const bh = (v / yMax) * area.h;
      const x = area.x + i * barW + barW * 0.12;
      const y = area.y + area.h - bh;
      ctx.fillStyle = color;
      ctx.globalAlpha = 0.82;
      ctx.fillRect(x, y, barW * 0.76, bh);
      ctx.globalAlpha = 1;
    });
  }

  // ── Stacked bar chart ────────────────────────────────────────────
  function drawStackedBars(canvas, points, seriesKeys, countsKey) {
    if (!points || points.length === 0) { drawEmpty(canvas); return; }
    const ctx = canvas.getContext('2d');
    ctx.clearRect(0, 0, canvas.width, canvas.height);

    const totals = points.map(p =>
      seriesKeys.reduce((s, k) => s + ((p[countsKey] || {})[k] || 0), 0)
    );
    const yMax = niceMax(Math.max(...totals));
    const area = ca(canvas);
    const barW = area.w / points.length;

    drawAxes(ctx, area, points.map(p => p.date), yMax);

    points.forEach((p, i) => {
      const counts = p[countsKey] || {};
      let yOff = 0;
      seriesKeys.forEach(k => {
        const v = counts[k] || 0;
        if (v === 0) return;
        const bh = (v / yMax) * area.h;
        const x = area.x + i * barW + barW * 0.12;
        const y = area.y + area.h - yOff - bh;
        ctx.fillStyle = typeColor(k);
        ctx.globalAlpha = 0.85;
        ctx.fillRect(x, y, barW * 0.76, bh);
        ctx.globalAlpha = 1;
        yOff += bh;
      });
    });
  }

  // ── Stacked area chart ───────────────────────────────────────────
  function drawStackedArea(canvas, points, seriesKeys) {
    if (!points || points.length === 0) { drawEmpty(canvas); return; }
    if (points.length === 1) {
      // Single snapshot — render as bar chart instead of an area with no width.
      const p = points[0];
      const fake = [{ date: p.date, counts: p.by_type }];
      drawStackedBars(canvas, fake, seriesKeys, 'counts');
      return;
    }
    const ctx = canvas.getContext('2d');
    ctx.clearRect(0, 0, canvas.width, canvas.height);

    const totals = points.map(p =>
      seriesKeys.reduce((s, k) => s + ((p.by_type || {})[k] || 0), 0)
    );
    const yMax = niceMax(Math.max(...totals));
    const area = ca(canvas);
    const n = points.length;
    const stepX = area.w / (n - 1);

    drawAxes(ctx, area, points.map(p => p.date), yMax);

    // Build cumulative baselines bottom-up.
    const baselines = new Array(n).fill(0);

    seriesKeys.forEach(k => {
      const vals = points.map((p, i) => baselines[i] + ((p.by_type || {})[k] || 0));

      ctx.beginPath();
      // Upper edge (forward)
      ctx.moveTo(area.x, area.y + area.h - (vals[0] / yMax) * area.h);
      for (let i = 1; i < n; i++) {
        ctx.lineTo(area.x + i * stepX, area.y + area.h - (vals[i] / yMax) * area.h);
      }
      // Lower edge (backward baseline)
      for (let i = n - 1; i >= 0; i--) {
        ctx.lineTo(area.x + i * stepX, area.y + area.h - (baselines[i] / yMax) * area.h);
      }
      ctx.closePath();
      ctx.fillStyle = typeColor(k);
      ctx.globalAlpha = 0.72;
      ctx.fill();
      ctx.globalAlpha = 1;

      // Stroke upper edge for definition.
      ctx.beginPath();
      ctx.strokeStyle = typeColor(k);
      ctx.lineWidth = 1.5;
      ctx.globalAlpha = 0.9;
      ctx.moveTo(area.x, area.y + area.h - (vals[0] / yMax) * area.h);
      for (let i = 1; i < n; i++) {
        ctx.lineTo(area.x + i * stepX, area.y + area.h - (vals[i] / yMax) * area.h);
      }
      ctx.stroke();
      ctx.globalAlpha = 1;

      vals.forEach((v, i) => { baselines[i] = v; });
    });
  }

  // ── Time-series helpers ──────────────────────────────────────────

  // Days between two "YYYY-MM-DD" strings.
  function daysBetween(a, b) {
    return (Date.parse(b + 'T00:00:00Z') - Date.parse(a + 'T00:00:00Z')) / 86400000;
  }

  // Trailing N-day rolling average (date-aware, not index-aware).
  // Returns an array the same length as vals.
  function rollingAvg(vals, dates, windowDays) {
    return vals.map((_, i) => {
      const end = Date.parse(dates[i] + 'T00:00:00Z');
      const start = end - (windowDays - 1) * 86400000;
      let sum = 0, count = 0;
      for (let j = i; j >= 0; j--) {
        const d = Date.parse(dates[j] + 'T00:00:00Z');
        if (d < start) break;
        sum += vals[j];
        count++;
      }
      return count > 0 ? sum / count : 0;
    });
  }

  // ── Multi-line chart (one line per series) ───────────────────────
  // opts.smooth      — apply 7-day rolling average to the line (default false)
  // opts.smoothWindow — rolling window in days (default 7)
  // opts.gapDays     — break line if consecutive points are this far apart (default 5)
  function drawLineChart(canvas, points, seriesKeys, countsKey, opts) {
    opts = opts || {};
    const smooth       = !!opts.smooth;
    const smoothWindow = opts.smoothWindow || 7;
    const gapDays      = opts.gapDays !== undefined ? opts.gapDays : 5;

    if (!points || points.length === 0) { drawEmpty(canvas); return; }
    const ctx = canvas.getContext('2d');
    ctx.clearRect(0, 0, canvas.width, canvas.height);

    const dates = points.map(p => p.date);
    const isSingle = points.length === 1;
    const stepX = isSingle ? 0 : area_w_for(canvas) / (points.length - 1);

    // Raw values per series — yMax always from raw so axis is stable on toggle.
    const rawBySeries = {};
    seriesKeys.forEach(k => {
      rawBySeries[k] = points.map(p => ((p[countsKey] || {})[k] || 0));
    });
    const allRaw = seriesKeys.flatMap(k => rawBySeries[k]);
    const yMax = niceMax(Math.max(...allRaw, 1));

    const area = ca(canvas);
    drawAxes(ctx, area, dates, yMax);

    seriesKeys.forEach(k => {
      const raw   = rawBySeries[k];
      const line  = (smooth && !isSingle) ? rollingAvg(raw, dates, smoothWindow) : raw;
      const color = typeColor(k);

      if (!isSingle) {
        // Gap-aware line (follows smoothed or raw values).
        ctx.beginPath();
        ctx.strokeStyle = color;
        ctx.lineWidth = 2;
        ctx.lineJoin = 'round';
        ctx.globalAlpha = 0.9;
        let penDown = false;
        line.forEach((v, i) => {
          const x = area.x + i * stepX;
          const y = area.y + area.h - (v / yMax) * area.h;
          const gap = i > 0 ? daysBetween(dates[i - 1], dates[i]) : 0;
          if (!penDown || gap > gapDays) {
            ctx.moveTo(x, y);
            penDown = true;
          } else {
            ctx.lineTo(x, y);
          }
        });
        ctx.stroke();
        ctx.globalAlpha = 1;
      }

      // Dots always at raw data positions — visible through the smooth line.
      ctx.fillStyle = color;
      ctx.globalAlpha = smooth ? 0.45 : 0.9;
      raw.forEach((v, i) => {
        if (v === 0 && !isSingle) return;
        const x = isSingle ? area.x + area.w / 2 : area.x + i * stepX;
        const y = area.y + area.h - (v / yMax) * area.h;
        ctx.beginPath();
        ctx.arc(x, y, isSingle ? 5 : 3, 0, Math.PI * 2);
        ctx.fill();
      });
      ctx.globalAlpha = 1;
    });
  }

  // Helper used inside drawLineChart before area is computed.
  function area_w_for(canvas) {
    return canvas.width - PAD.left - PAD.right;
  }

  // ── Empty state ──────────────────────────────────────────────────
  function drawEmpty(canvas) {
    const ctx = canvas.getContext('2d');
    ctx.clearRect(0, 0, canvas.width, canvas.height);
    ctx.fillStyle = '#3a3a5a';
    ctx.font = '12px monospace';
    ctx.textAlign = 'center';
    ctx.textBaseline = 'middle';
    ctx.fillText('no data', canvas.width / 2, canvas.height / 2);
  }

  // ── Legend ───────────────────────────────────────────────────────
  function drawLegend(containerId, keys) {
    const el = document.getElementById(containerId);
    if (!el) return;
    el.innerHTML = keys.map(k =>
      `<span class="legend-item"><span class="legend-dot" style="background:${typeColor(k)}"></span>${k}</span>`
    ).join('');
  }

  // ── Tooltip ──────────────────────────────────────────────────────
  function bindTooltip(canvas, points, labelFn) {
    const area = ca(canvas);

    function onMove(e) {
      const rect = canvas.getBoundingClientRect();
      const scaleX = canvas.width / rect.width;
      const mx = (e.clientX - rect.left) * scaleX;
      const barW = area.w / points.length;
      const idx = Math.floor((mx - area.x) / barW);
      if (idx < 0 || idx >= points.length) { tip.style.display = 'none'; return; }
      tip.style.display = 'block';
      tip.style.left = (e.clientX + 14) + 'px';
      tip.style.top = (e.clientY - 10) + 'px';
      tip.innerHTML = labelFn(points[idx]);
    }

    canvas.addEventListener('mousemove', onMove);
    canvas.addEventListener('mouseleave', () => { tip.style.display = 'none'; });
  }

  // ── Resize + redraw ──────────────────────────────────────────────
  const CANVAS_IDS = ['chart-recall', 'chart-promotes', 'chart-depth', 'chart-daydream', 'chart-body-hits'];

  function resizeAll() {
    CANVAS_IDS.forEach(id => {
      const c = document.getElementById(id);
      if (c) c.width = c.parentElement.clientWidth;
    });
  }

  // ── Recall smooth toggle ─────────────────────────────────────────
  let recallSmoothed = true; // default: 7-day rolling average

  function redrawRecall(data) {
    const recallData   = (data || liveData).recall_by_day || [];
    const recallCanvas = document.getElementById('chart-recall');
    if (!recallCanvas) return;
    const allTypes = [...new Set(recallData.flatMap(p => Object.keys(p.counts || {})))].sort();
    drawLineChart(recallCanvas, recallData, allTypes, 'counts', { smooth: recallSmoothed });
    drawLegend('legend-recall', allTypes);
    bindTooltip(recallCanvas, recallData, p => {
      const rows = Object.entries(p.counts || {}).map(([k, v]) => `${k}: <b>${v}</b>`).join('<br>');
      const bodyHits = (p.avg_body_hits || 0).toFixed(2);
      return `<b>${p.date}</b><br>${rows}<br>prompts: <b>${p.prompts}</b>&nbsp; avg: <b>${p.avg_recall.toFixed(1)}</b>&nbsp; body-hits: <b>${bodyHits}</b>`;
    });
    const metaEl = document.getElementById('meta-recall');
    if (metaEl) {
      const totalPrompts = recallData.reduce((s, p) => s + p.prompts, 0);
      const totalRecalls = recallData.reduce((s, p) => s + p.total, 0);
      metaEl.textContent = `${totalRecalls} total across ${totalPrompts} prompts`;
    }
  }

  function initRecallToggle() {
    const btn = document.getElementById('recall-smooth-toggle');
    if (!btn) return;
    function sync() {
      btn.textContent = recallSmoothed ? '7d avg' : 'raw';
      btn.classList.toggle('active', recallSmoothed);
    }
    btn.addEventListener('click', function () {
      recallSmoothed = !recallSmoothed;
      sync();
      redrawRecall();
    });
    sync();
  }

  // ── Live mode helpers ────────────────────────────────────────────
  let liveData = DATA; // mutable reference updated by SSE events

  function setLiveStatus(state) {
    // state: 'connected' | 'stale' | 'error'
    const dot = document.getElementById('live-dot');
    const label = document.getElementById('live-label');
    if (!dot || !label) return;
    dot.className = state === 'connected' ? '' : state;
    const labels = { connected: 'live', stale: 'reconnecting…', error: 'disconnected' };
    label.textContent = labels[state] || state;
  }

  function updateTimestamp(iso) {
    const el = document.getElementById('generated-at');
    if (el && iso) el.textContent = iso.replace('T', ' ').replace('Z', ' UTC');
  }

  function connectSSE() {
    const indicator = document.getElementById('live-indicator');
    if (indicator) indicator.style.display = 'flex';

    const es = new EventSource('/events');

    es.addEventListener('open', function () {
      setLiveStatus('connected');
    });

    es.addEventListener('message', function (e) {
      try {
        const newPayload = JSON.parse(e.data);
        liveData = newPayload;
        resizeAll();
        renderAllWith(liveData);
        updateTimestamp(newPayload.meta && newPayload.meta.generated_at);
        setLiveStatus('connected');
      } catch (err) {
        console.error('[dashboard] SSE parse error:', err);
      }
    });

    es.addEventListener('error', function () {
      setLiveStatus(es.readyState === EventSource.CONNECTING ? 'stale' : 'error');
    });
  }

  // renderAllWith allows passing an explicit data object (used by SSE updates).
  function renderAllWith(d) {
    const promoteData  = (d.promotes_by_day || []);
    const depthData    = (d.vault_depth     || []);
    const daydreamData = (d.daydream_by_day || []);

    // Recall chart delegated to redrawRecall so toggle re-uses the same path.
    redrawRecall(d);

    const promoteCanvas = document.getElementById('chart-promotes');
    if (promoteCanvas) {
      drawBarChart(promoteCanvas, promoteData, 'count', '#96ceb4');
      bindTooltip(promoteCanvas, promoteData, p =>
        `<b>${p.date}</b><br>promotions: <b>${p.count}</b>`
      );
      const metaEl = document.getElementById('meta-promotes');
      if (metaEl) metaEl.textContent = `${promoteData.reduce((s, p) => s + p.count, 0)} total`;
    }

    const depthCanvas = document.getElementById('chart-depth');
    if (depthCanvas) {
      const typeOrder = ['knowledge','semantic','user','feedback','project','episodic','reference'];
      const presentTypes = typeOrder.filter(k => depthData.some(p => (p.by_type || {})[k]));
      drawStackedArea(depthCanvas, depthData, presentTypes);
      drawLegend('legend-depth', presentTypes);
      bindTooltip(depthCanvas, depthData, p => {
        const rows = Object.entries(p.by_type || {}).sort((a,b) => b[1]-a[1]).map(([k,v]) => `${k}: <b>${v}</b>`).join('<br>');
        return `<b>${p.date}</b><br>total: <b>${p.total}</b><br>${rows}`;
      });
      if (depthData.length > 0) {
        const last = depthData[depthData.length - 1];
        const metaEl = document.getElementById('meta-depth');
        if (metaEl) metaEl.textContent = `${last.total} memories as of ${last.date}`;
      }
    }

    const daydreamCanvas = document.getElementById('chart-daydream');
    if (daydreamCanvas) {
      drawBarChart(daydreamCanvas, daydreamData, 'count', '#c084fc');
      bindTooltip(daydreamCanvas, daydreamData, p =>
        `<b>${p.date}</b><br>daydreams: <b>${p.count}</b>`
      );
      const metaEl = document.getElementById('meta-daydream');
      if (metaEl) metaEl.textContent = `${daydreamData.reduce((s, p) => s + p.count, 0)} total`;
    }

    const bodyHitsCanvas = document.getElementById('chart-body-hits');
    if (bodyHitsCanvas) {
      const recallData = (d.recall_by_day || []);
      // Draw avg_body_hits as a bar chart — each bar = mean keyword hits per recalled memory that day.
      // 0.0 means all retrievals were tag-only (no body contact); higher values indicate
      // prompt keywords found in memory body text. Semantic framing memories score 0 regardless
      // of actual influence — this chart captures episodic/factual activation depth only.
      drawBarChart(bodyHitsCanvas, recallData, 'avg_body_hits', '#60a5fa');
      bindTooltip(bodyHitsCanvas, recallData, p => {
        const hits = (p.avg_body_hits || 0).toFixed(2);
        const rel  = (p.avg_relevance || 0).toFixed(2);
        return `<b>${p.date}</b><br>avg body hits: <b>${hits}</b><br>avg relevance: <b>${rel}</b><br>prompts: <b>${p.prompts}</b>`;
      });
    }
  }

  // ── Init ─────────────────────────────────────────────────────────
  window.addEventListener('load', function () {
    updateTimestamp(DATA.meta && DATA.meta.generated_at);

    const graphLink = document.getElementById('graph-link');
    if (graphLink && DATA.graph_rel_path) {
      graphLink.href = DATA.graph_rel_path;
    }

    resizeAll();
    initRecallToggle();
    renderAllWith(liveData);

    if (window.__DASHBOARD_MODE__ === 'live') {
      connectSSE();
    }
  });

  // Debounced resize.
  let resizeTimer;
  window.addEventListener('resize', function () {
    clearTimeout(resizeTimer);
    resizeTimer = setTimeout(function () {
      resizeAll();
      renderAllWith(liveData);
    }, 120);
  });
})();
