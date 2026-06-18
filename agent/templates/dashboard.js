(function () {
  'use strict';

  let liveData = {};
  try {
    const dataEl = document.getElementById('dashboard-data');
    if (dataEl && dataEl.textContent) {
      liveData = JSON.parse(dataEl.textContent);
    }
  } catch (err) {
    console.error('[dashboard] failed to parse embedded metrics data:', err);
  }
  const tip = document.getElementById('tooltip');
  const crosshair = document.getElementById('crosshair');

  const TYPE_COLORS = {
    episodic:  '#e07b54',
    feedback:  '#4ecdc4',
    knowledge: '#45b7d1',
    project:   '#96ceb4',
    reference: '#f0c94a',
    semantic:  '#c084fc',
    user:      '#6ee7b7',
  };
  const TYPE_ORDER = ['knowledge','semantic','user','feedback','project','episodic','reference'];
  const FALLBACK_COLORS = ['#8b9dc3','#f7a4a4','#a4c4f7','#d4a4f7','#a4f7c4'];
  let fallbackIdx = 0;
  function typeColor(t) {
    if (!TYPE_COLORS[t]) {
      TYPE_COLORS[t] = FALLBACK_COLORS[fallbackIdx++ % FALLBACK_COLORS.length];
    }
    return TYPE_COLORS[t];
  }

  const PAD = { top: 22, right: 52, bottom: 40, left: 48 };
  const RECALL_CHART_START = '2026-05-15';

  let rangeDays = 30;
  let smoothLines = true;
  let usageLiveOnly = true;
  let activeTypes = new Set(TYPE_ORDER);
  let hoverDate = null;

  // Wide charts share a recall/usage timeline; smaller charts use canvas-local hover lines only.
  const WIDE_CROSSHAIR_IDS = new Set(['chart-hero', 'chart-adherence']);

  function sliceRange(arr) {
    if (!arr) return [];
    if (rangeDays === 0 || arr.length <= rangeDays) return arr;
    return arr.slice(-rangeDays);
  }

  function recallChartData(data) {
    return sliceRange((data || liveData).recall_by_day || [])
      .filter(p => p.date >= RECALL_CHART_START);
  }

  function filtered() {
    const d = liveData || {};
    return {
      recall: recallChartData(d),
      usage: sliceRange(d.usage_by_day || []),
      models: d.usage_by_model || [],
      usageSummary: d.usage_summary || {},
      promotes: sliceRange(d.promotes_by_day || []),
      depth: sliceRange(d.vault_depth || []),
      daydream: sliceRange(d.daydream_by_day || []),
    };
  }

  function idxByDate(points, date) {
    if (!date || !points) return -1;
    return points.findIndex(p => p.date === date);
  }

  function adherenceDates(data) {
    const dateSet = new Set();
    (data.usage || []).forEach(p => dateSet.add(p.date));
    (data.recall || []).forEach(p => dateSet.add(p.date));
    return [...dateSet].sort();
  }

  function adherencePoints(data) {
    const usageByDate = Object.fromEntries((data.usage || []).map(p => [p.date, p]));
    const recallByDate = Object.fromEntries((data.recall || []).map(p => [p.date, p]));
    return adherenceDates(data).map(date => {
      const u = usageByDate[date] || {};
      const r = recallByDate[date] || {};
      return {
        date,
        injected: usageLiveOnly ? (u.live_injected || 0) : (u.injected || 0),
        referenced: usageLiveOnly ? (u.live_referenced || 0) : (u.referenced || 0),
        live_turns: u.live_turns || 0,
        backfill_turns: u.backfill_turns || 0,
        zero_recall_prompts: r.zero_recall_prompts || 0,
      };
    });
  }

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
    return step * magnitude * 1.08;
  }

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

  function drawEmpty(canvas) {
    const ctx = canvas.getContext('2d');
    ctx.clearRect(0, 0, canvas.width, canvas.height);
    ctx.fillStyle = '#4a4a6a';
    ctx.font = '12px ui-monospace, monospace';
    ctx.textAlign = 'center';
    ctx.textBaseline = 'middle';
    ctx.fillText('no data', canvas.width / 2, canvas.height / 2);
  }

  function drawGrid(ctx, area, xLabels, yMax, yMaxR) {
    const steps = 4;
    ctx.save();
    ctx.font = '10px ui-monospace, monospace';

    for (let i = 0; i <= steps; i++) {
      const v = (yMax / steps) * i;
      const y = area.y + area.h - (i / steps) * area.h;
      ctx.beginPath();
      ctx.setLineDash([2, 5]);
      ctx.strokeStyle = 'rgba(255,255,255,0.05)';
      ctx.moveTo(area.x, y);
      ctx.lineTo(area.x + area.w, y);
      ctx.stroke();
      ctx.setLineDash([]);

      ctx.fillStyle = '#7a7a9a';
      ctx.textAlign = 'right';
      ctx.textBaseline = 'middle';
      const label = v >= 1000 ? (v / 1000).toFixed(1) + 'k' : String(Math.round(v));
      ctx.fillText(label, area.x - 5, y);
    }

    if (yMaxR !== undefined) {
      for (let i = 0; i <= steps; i++) {
        const v = (yMaxR / steps) * i;
        const y = area.y + area.h - (i / steps) * area.h;
        ctx.fillStyle = '#7a7a9a';
        ctx.textAlign = 'left';
        ctx.fillText(v.toFixed(2), area.x + area.w + 6, y);
      }
    }

    const maxLabels = Math.max(2, Math.floor(area.w / 50));
    const step = Math.max(1, Math.ceil(xLabels.length / maxLabels));
    const barW = area.w / Math.max(xLabels.length, 1);
    ctx.fillStyle = '#7a7a9a';
    ctx.textAlign = 'center';
    ctx.textBaseline = 'top';
    xLabels.forEach((label, i) => {
      if (i % step !== 0) return;
      ctx.fillText(label.slice(5), area.x + (i + 0.5) * barW, area.y + area.h + 6);
    });

    ctx.beginPath();
    ctx.strokeStyle = '#2e2e4a';
    ctx.lineWidth = 1;
    ctx.moveTo(area.x, area.y);
    ctx.lineTo(area.x, area.y + area.h);
    ctx.lineTo(area.x + area.w, area.y + area.h);
    ctx.stroke();
    ctx.restore();
  }

  function drawHoverLine(ctx, area, idx, n) {
    if (idx < 0 || idx >= n) return;
    const x = area.x + (idx + 0.5) * (area.w / n);
    ctx.save();
    ctx.strokeStyle = 'rgba(139,146,240,0.55)';
    ctx.lineWidth = 1;
    ctx.setLineDash([4, 4]);
    ctx.beginPath();
    ctx.moveTo(x, area.y);
    ctx.lineTo(x, area.y + area.h);
    ctx.stroke();
    ctx.restore();
  }

  function drawHero(canvas, data, hi) {
    const points = data.recall;
    if (!points || points.length === 0) { drawEmpty(canvas); return; }

    const ctx = canvas.getContext('2d');
    ctx.clearRect(0, 0, canvas.width, canvas.height);

    const dates = points.map(p => p.date);
    const types = TYPE_ORDER.filter(t => activeTypes.has(t));
    const totals = points.map(p => types.reduce((s, k) => s + ((p.counts || {})[k] || 0), 0));
    const yMax = niceMax(Math.max(...totals, 1));

    const relRaw = points.map(p => p.avg_relevance || 0);
    const bodyRaw = points.map(p => p.avg_body_hits || 0);
    const relLine = smoothLines ? rollingAvg(relRaw, dates, 7) : relRaw;
    const bodyLine = smoothLines ? rollingAvg(bodyRaw, dates, 7) : bodyRaw;
    const yMaxR = Math.max(...relLine, ...bodyLine, 0.01) * 1.15;

    const area = ca(canvas);
    const n = points.length;
    const barW = area.w / n;

    drawGrid(ctx, area, dates, yMax, yMaxR);

    points.forEach((p, i) => {
      let yOff = 0;
      types.forEach(k => {
        const v = (p.counts || {})[k] || 0;
        if (!v) return;
        const bh = (v / yMax) * area.h;
        const x = area.x + i * barW + barW * 0.15;
        const y = area.y + area.h - yOff - bh;
        ctx.fillStyle = typeColor(k);
        ctx.globalAlpha = i === hi ? 1 : 0.82;
        ctx.fillRect(x, y, barW * 0.7, bh);
        ctx.globalAlpha = 1;
        yOff += bh;
      });
    });

    function drawLine(vals, color, dashed) {
      ctx.beginPath();
      ctx.strokeStyle = color;
      ctx.lineWidth = 2.5;
      ctx.lineJoin = 'round';
      if (dashed) ctx.setLineDash([6, 4]);
      vals.forEach((v, i) => {
        const x = area.x + (i + 0.5) * barW;
        const y = area.y + area.h - (v / yMaxR) * area.h;
        i === 0 ? ctx.moveTo(x, y) : ctx.lineTo(x, y);
      });
      ctx.stroke();
      ctx.setLineDash([]);
      vals.forEach((v, i) => {
        const x = area.x + (i + 0.5) * barW;
        const y = area.y + area.h - (v / yMaxR) * area.h;
        ctx.beginPath();
        ctx.fillStyle = color;
        ctx.globalAlpha = smoothLines ? 0.55 : 0.9;
        ctx.arc(x, y, i === hi ? 5 : 3, 0, Math.PI * 2);
        ctx.fill();
      });
      ctx.globalAlpha = 1;
    }

    drawLine(relLine, '#f59e0b', false);
    drawLine(bodyLine, '#60a5fa', true);
    drawHoverLine(ctx, area, hi, n);
  }

  function drawVault(canvas, data, hi) {
    const points = data.depth;
    if (!points || points.length === 0) { drawEmpty(canvas); return; }

    const ctx = canvas.getContext('2d');
    ctx.clearRect(0, 0, canvas.width, canvas.height);

    if (points.length === 1) {
      const p = points[0];
      const fake = [{ date: p.date, counts: p.by_type, total: p.total }];
      drawStackedBars(canvas, fake, TYPE_ORDER.filter(t => (p.by_type || {})[t]), 'counts');
      return;
    }

    const types = TYPE_ORDER.filter(t => points.some(p => (p.by_type || {})[t]));
    const totals = points.map(p => p.total);
    const yMax = niceMax(Math.max(...totals));
    const area = ca(canvas);
    const n = points.length;
    const stepX = area.w / (n - 1);
    const dates = points.map(p => p.date);

    drawGrid(ctx, area, dates, yMax);

    const baselines = new Array(n).fill(0);
    types.forEach(k => {
      const vals = points.map((p, i) => baselines[i] + ((p.by_type || {})[k] || 0));
      ctx.beginPath();
      ctx.moveTo(area.x, area.y + area.h - (vals[0] / yMax) * area.h);
      for (let i = 1; i < n; i++) {
        ctx.lineTo(area.x + i * stepX, area.y + area.h - (vals[i] / yMax) * area.h);
      }
      for (let i = n - 1; i >= 0; i--) {
        ctx.lineTo(area.x + i * stepX, area.y + area.h - (baselines[i] / yMax) * area.h);
      }
      ctx.closePath();
      ctx.fillStyle = typeColor(k);
      ctx.globalAlpha = 0.72;
      ctx.fill();
      ctx.globalAlpha = 1;
      vals.forEach((v, i) => { baselines[i] = v; });
    });

    ctx.beginPath();
    ctx.strokeStyle = '#e4e4f4';
    ctx.lineWidth = 2;
    ctx.globalAlpha = 0.85;
    totals.forEach((v, i) => {
      const x = area.x + i * stepX;
      const y = area.y + area.h - (v / yMax) * area.h;
      i === 0 ? ctx.moveTo(x, y) : ctx.lineTo(x, y);
    });
    ctx.stroke();
    ctx.globalAlpha = 1;

    if (hi >= 0) {
      const x = area.x + hi * stepX;
      ctx.strokeStyle = 'rgba(139,146,240,0.55)';
      ctx.setLineDash([4, 4]);
      ctx.beginPath();
      ctx.moveTo(x, area.y);
      ctx.lineTo(x, area.y + area.h);
      ctx.stroke();
      ctx.setLineDash([]);
    }
  }

  function drawStackedBars(canvas, points, seriesKeys, countsKey) {
    if (!points || points.length === 0) { drawEmpty(canvas); return; }
    const ctx = canvas.getContext('2d');
    ctx.clearRect(0, 0, canvas.width, canvas.height);

    const totals = points.map(p =>
      seriesKeys.reduce((s, k) => s + ((p[countsKey] || {})[k] || 0), 0)
    );
    const yMax = niceMax(Math.max(...totals, 1));
    const area = ca(canvas);
    const barW = area.w / points.length;

    drawGrid(ctx, area, points.map(p => p.date), yMax);

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

  function drawAdherence(canvas, data, hi) {
    const usagePts = data.usage || [];
    const recallPts = data.recall || [];
    if ((!usagePts.length) && (!recallPts.length)) { drawEmpty(canvas); return; }

    const dates = adherenceDates(data);
    const usageByDate = Object.fromEntries(usagePts.map(p => [p.date, p]));
    const recallByDate = Object.fromEntries(recallPts.map(p => [p.date, p]));

    const injected = dates.map(d => {
      const u = usageByDate[d] || {};
      return usageLiveOnly ? (u.live_injected || 0) : (u.injected || 0);
    });
    const referenced = dates.map(d => {
      const u = usageByDate[d] || {};
      return usageLiveOnly ? (u.live_referenced || 0) : (u.referenced || 0);
    });
    const zeroRecall = dates.map(d => (recallByDate[d] || {}).zero_recall_prompts || 0);

    const ctx = canvas.getContext('2d');
    ctx.clearRect(0, 0, canvas.width, canvas.height);

    const yMax = niceMax(Math.max(...injected, ...referenced, ...zeroRecall, 1));
    const area = ca(canvas);
    const n = dates.length;
    const barW = area.w / Math.max(n, 1);

    drawGrid(ctx, area, dates, yMax);

    dates.forEach((_, i) => {
      const day = usageByDate[dates[i]] || {};
      const backfillOnly = !usageLiveOnly &&
        (day.backfill_turns || 0) > 0 && (day.live_turns || 0) === 0;
      const injH = (injected[i] / yMax) * area.h;
      const refH = (referenced[i] / yMax) * area.h;
      const x = area.x + i * barW;
      const injX = x + barW * 0.12;
      const refX = x + barW * 0.42;

      ctx.fillStyle = '#a78bfa';
      ctx.globalAlpha = backfillOnly ? (i === hi ? 0.72 : 0.48) : (i === hi ? 1 : 0.75);
      ctx.fillRect(injX, area.y + area.h - injH, barW * 0.26, injH);

      ctx.fillStyle = '#fb923c';
      ctx.globalAlpha = backfillOnly ? (i === hi ? 0.72 : 0.48) : (i === hi ? 1 : 0.75);
      ctx.fillRect(refX, area.y + area.h - refH, barW * 0.26, refH);

      if (zeroRecall[i] > 0) {
        const zH = (zeroRecall[i] / yMax) * area.h;
        ctx.fillStyle = '#f87171';
        ctx.globalAlpha = 0.9;
        ctx.fillRect(x + barW * 0.72, area.y + area.h - zH, barW * 0.18, zH);
      }
      ctx.globalAlpha = 1;
    });

    drawHoverLine(ctx, area, hi, n);
  }

  function buildAdherenceLegend() {
    const el = document.getElementById('adherence-legend');
    if (!el) return;
    el.innerHTML =
      '<span class="legend-item"><span class="legend-swatch" style="background:#a78bfa"></span>injected (hook)</span>' +
      '<span class="legend-item"><span class="legend-swatch" style="background:#fb923c"></span>referenced (model)</span>' +
      '<span class="legend-item"><span class="legend-swatch" style="background:#f87171"></span>zero-recall prompts</span>' +
      '<span class="legend-item"><span class="legend-swatch" style="background:#a78bfa;opacity:0.45"></span>backfill-only days (faded)</span>';
  }

  function updateAdherenceCallout(data) {
    const el = document.getElementById('adherence-callout');
    if (!el) return;
    const s = data.usageSummary || {};
    if (!s.total_turns) {
      el.innerHTML = 'No usage telemetry yet. Run hooks live or <b>jm metrics backfill-usage</b> to replay retrieval sessions against saved transcripts.';
      return;
    }
    const parts = [
      `<b>${s.total_turns}</b> usage-scored turns`,
      `<b>${s.live_turns || 0}</b> live`,
      `<b>${s.backfill_turns || 0}</b> backfill`,
    ];
    if (s.usage_coverage_from) {
      parts.push(`coverage <b>${s.usage_coverage_from}</b>→<b>${s.usage_coverage_to || s.usage_coverage_from}</b>`);
    }
    if (s.injection_only_before) {
      parts.push(`<span class="coverage-gap">injection-only before ${s.injection_only_before}</span>`);
    }
    const livePct = ((s.live_usage_rate || 0) * 100).toFixed(0);
    const backPct = ((s.backfill_usage_rate || 0) * 100).toFixed(0);
    if (usageLiveOnly) {
      parts.push(`live rate <b>${livePct}%</b> <span style="color:var(--muted)">(backfill ${backPct}% hidden)</span>`);
    } else {
      parts.push(`rates: live <b>${livePct}%</b> · backfill <b>${backPct}%</b>`);
    }
    el.innerHTML = parts.join(' · ');
  }

  function modelRowStats(m) {
    if (usageLiveOnly) {
      return {
        turns: m.live_turns || 0,
        injected: m.live_injected || 0,
        referenced: m.live_referenced || 0,
        rate: m.live_usage_rate || 0,
        zero: m.live_zero_usage_turns || 0,
      };
    }
    return {
      turns: m.turns || 0,
      injected: m.injected || 0,
      referenced: m.referenced || 0,
      rate: m.usage_rate || 0,
      zero: m.zero_usage_turns || 0,
    };
  }

  function modelSourceBadge(m) {
    const live = m.live_turns || 0;
    const back = m.backfill_turns || 0;
    if (live > 0 && back === 0) return '<span class="source-badge source-live">live</span>';
    if (back > 0 && live === 0) return '<span class="source-badge source-backfill">backfill</span>';
    if (live > 0 && back > 0) return `<span class="source-badge source-mixed">${live}L/${back}B</span>`;
    return '—';
  }

  function updateModelTable(data) {
    const tbody = document.getElementById('model-table-body');
    if (!tbody) return;
    const models = (data.models || []).filter(m => !usageLiveOnly || (m.live_turns || 0) > 0);
    if (!models.length) {
      const msg = usageLiveOnly
        ? 'No live usage telemetry yet — run hooks or disable <b>live only</b> to include backfill'
        : 'No usage telemetry yet — requires retrieval_session_log + stop/user-prompt-submit hooks';
      tbody.innerHTML = `<tr><td colspan="6" style="color:var(--muted);padding:16px">${msg}</td></tr>`;
      return;
    }
    tbody.innerHTML = models.map(m => {
      const s = modelRowStats(m);
      const cls = s.rate >= 0.5 ? 'usage-good' : s.rate >= 0.2 ? 'usage-warn' : 'usage-bad';
      return `<tr>
        <td class="model-name">${m.model || 'unknown'}</td>
        <td>${m.runtime_host || '—'}</td>
        <td>${s.turns}</td>
        <td>${modelSourceBadge(m)}</td>
        <td class="${cls}">${(s.rate * 100).toFixed(0)}% <span style="color:var(--muted)">(${s.referenced}/${s.injected})</span></td>
        <td>${s.zero}</td>
      </tr>`;
    }).join('');
  }

  function drawBars(canvas, points, color, hi) {
    if (!points || points.length === 0) { drawEmpty(canvas); return; }
    const ctx = canvas.getContext('2d');
    ctx.clearRect(0, 0, canvas.width, canvas.height);

    const vals = points.map(p => p.count);
    const yMax = niceMax(Math.max(...vals, 1));
    const area = ca(canvas);
    const n = points.length;
    const barW = area.w / n;
    const dates = points.map(p => p.date);

    drawGrid(ctx, area, dates, yMax);

    vals.forEach((v, i) => {
      const bh = (v / yMax) * area.h;
      const x = area.x + i * barW + barW * 0.18;
      const y = area.y + area.h - bh;
      ctx.fillStyle = color;
      ctx.globalAlpha = i === hi ? 1 : 0.78;
      ctx.fillRect(x, y, barW * 0.64, bh);
      ctx.globalAlpha = 1;
    });
    drawHoverLine(ctx, area, hi, n);
  }

  function updateKPIs(data) {
    const r = data.recall;
    const totalRecalls = r.reduce((s, p) => s + p.total, 0);
    const totalPrompts = r.reduce((s, p) => s + p.prompts, 0);
    const days = r.length || 1;
    const recallTotal = Math.max(r.reduce((s, p) => s + p.total, 0), 1);
    const avgBody = r.reduce((s, p) => s + p.avg_body_hits * p.total, 0) / recallTotal;

    const el = id => document.getElementById(id);
    if (el('kpi-recalls')) el('kpi-recalls').textContent = totalRecalls.toLocaleString();
    if (el('kpi-recalls-sub')) el('kpi-recalls-sub').textContent = `${days}d · ${totalPrompts} prompts`;
    if (el('kpi-prompts')) el('kpi-prompts').textContent = (totalPrompts / days).toFixed(1);
    if (el('kpi-bodyhits')) el('kpi-bodyhits').textContent = avgBody.toFixed(2);

    const last7 = r.slice(-7);
    const prev7 = r.slice(-14, -7);
    const body7 = last7.reduce((s, p) => s + p.avg_body_hits * p.total, 0) /
      Math.max(last7.reduce((s, p) => s + p.total, 0), 1);
    const bodyPrev = prev7.length
      ? prev7.reduce((s, p) => s + p.avg_body_hits * p.total, 0) /
        Math.max(prev7.reduce((s, p) => s + p.total, 0), 1)
      : body7;
    const bodyDelta = body7 - bodyPrev;
    const bd = el('kpi-bodyhits-delta');
    if (bd) {
      bd.textContent = (bodyDelta >= 0 ? '▲ ' : '▼ ') + Math.abs(bodyDelta).toFixed(2);
      bd.className = 'kpi-delta ' + (bodyDelta > 0.02 ? 'up' : bodyDelta < -0.02 ? 'down' : 'flat');
    }

    const promptRate = totalPrompts / days;
    const prevPrompts = prev7.reduce((s, p) => s + p.prompts, 0);
    const prevRate = prev7.length ? prevPrompts / prev7.length : promptRate;
    const pd = el('kpi-prompts-delta');
    if (pd) {
      const pDelta = promptRate - prevRate;
      pd.textContent = (pDelta >= 0 ? '▲ ' : '▼ ') + Math.abs(pDelta).toFixed(1);
      pd.className = 'kpi-delta ' + (pDelta > 0.3 ? 'up' : pDelta < -0.3 ? 'down' : 'flat');
    }

    const depth = data.depth;
    if (depth.length) {
      const last = depth[depth.length - 1];
      const first = depth[0];
      if (el('kpi-vault')) el('kpi-vault').textContent = last.total.toLocaleString();
      if (el('kpi-vault-sub')) el('kpi-vault-sub').textContent = `+${last.total - first.total} in range`;
    }

    const usage = data.usage || [];
    const summary = data.usageSummary || {};
    let inj = usage.reduce((s, p) => s + (p.injected || 0), 0);
    let ref = usage.reduce((s, p) => s + (p.referenced || 0), 0);
    let turns = usage.reduce((s, p) => s + (p.turns || 0), 0);
    if (usageLiveOnly) {
      inj = usage.reduce((s, p) => s + (p.live_injected || 0), 0);
      ref = usage.reduce((s, p) => s + (p.live_referenced || 0), 0);
      turns = usage.reduce((s, p) => s + (p.live_turns || 0), 0);
    }
    const usageRate = inj > 0 ? ref / inj : (usageLiveOnly ? (summary.live_usage_rate || 0) : (summary.overall_usage_rate || 0));
    if (el('kpi-usage')) el('kpi-usage').textContent = (usageRate * 100).toFixed(0) + '%';
    if (el('kpi-usage-sub')) {
      const zeroUse = usage.reduce((s, p) => s + (p.zero_usage_turns || 0), 0);
      const src = usageLiveOnly ? 'live' : `${summary.backfill_turns || 0} backfill`;
      el('kpi-usage-sub').textContent = `${ref}/${inj} refs · ${turns} turns · ${src}`;
    }

    const buf = liveData.buffer || { count: 0, threshold: 10 };
    if (el('kpi-buffer')) el('kpi-buffer').textContent = `${buf.count}/${buf.threshold}`;
    const pct = buf.threshold > 0 ? Math.min(100, buf.count / buf.threshold * 100) : 0;
    if (el('kpi-buffer-sub')) {
      el('kpi-buffer-sub').textContent = pct >= 100 ? '⚠ threshold reached' : `${Math.round(pct)}% fill`;
    }
  }

  function updateVaultCallout(data) {
    const el = document.getElementById('vault-callout');
    if (!el) return;
    if (!data.depth.length) { el.textContent = ''; return; }
    const last = data.depth[data.depth.length - 1];
    const first = data.depth[0];
    const growth = last.total - first.total;
    el.innerHTML = `<b>${last.total}</b> memories as of ${last.date} · <b>+${growth}</b> over selected range`;
  }

  function buildLegend() {
    const el = document.getElementById('hero-legend');
    if (!el) return;
    const recallData = recallChartData();
    const presentTypes = [...new Set(recallData.flatMap(p => Object.keys(p.counts || {})))].sort();
    const typeItems = presentTypes.map(t =>
      `<span class="legend-item${activeTypes.has(t) ? '' : ' dimmed'}" data-type="${t}">` +
      `<span class="legend-swatch" style="background:${typeColor(t)}"></span>${t}</span>`
    ).join('');
    const lineItems =
      '<span class="legend-item"><span class="legend-line" style="background:#f59e0b"></span>relevance</span>' +
      '<span class="legend-item"><span class="legend-line" style="background:#60a5fa;opacity:0.7"></span>body hits</span>';
    el.innerHTML = typeItems + lineItems;
    el.querySelectorAll('[data-type]').forEach(item => {
      item.addEventListener('click', () => {
        const t = item.dataset.type;
        if (activeTypes.has(t)) {
          activeTypes.delete(t);
          item.classList.add('dimmed');
        } else {
          activeTypes.add(t);
          item.classList.remove('dimmed');
        }
        render();
      });
    });
  }

  function showTip(e, html) {
    tip.style.display = 'block';
    tip.innerHTML = html;
    tip.style.left = Math.min(e.clientX + 14, window.innerWidth - 280) + 'px';
    tip.style.top = Math.min(e.clientY - 10, window.innerHeight - 200) + 'px';
  }

  function hideTip() { tip.style.display = 'none'; }

  function recallTip(p) {
    const rows = Object.entries(p.counts || {})
      .sort((a, b) => b[1] - a[1])
      .map(([k, v]) =>
        `<div class="tip-row"><span><span class="tip-dot" style="background:${typeColor(k)}"></span>${k}</span><b>${v}</b></div>`
      ).join('');
    return `<div class="tip-date">${p.date}</div>${rows}` +
      `<div class="tip-section">prompts <b>${p.prompts}</b> · avg recall <b>${(p.avg_recall || 0).toFixed(1)}</b><br>` +
      `relevance <b>${(p.avg_relevance || 0).toFixed(2)}</b> · body hits <b>${(p.avg_body_hits || 0).toFixed(2)}</b></div>`;
  }

  function crosshairBounds(canvasId) {
    if (WIDE_CROSSHAIR_IDS.has(canvasId)) {
      const hero = document.getElementById('hero-panel');
      const adherence = document.getElementById('adherence-panel');
      if (hero && adherence) {
        const top = hero.getBoundingClientRect().top;
        const bottom = adherence.getBoundingClientRect().bottom;
        return { top, height: bottom - top };
      }
    }
    const canvas = document.getElementById(canvasId);
    const wrap = canvas && (canvas.closest('.canvas-wrap') || canvas.closest('.panel-body'));
    if (wrap) {
      const r = wrap.getBoundingClientRect();
      return { top: r.top, height: r.height };
    }
    return null;
  }

  function setCrosshair(canvas, idx, points, date) {
    if (idx < 0 || !points.length) {
      crosshair.style.display = 'none';
      return;
    }
    if (!WIDE_CROSSHAIR_IDS.has(canvas.id)) {
      crosshair.style.display = 'none';
      return;
    }
    const area = ca(canvas);
    const barW = area.w / points.length;
    const rect = canvas.getBoundingClientRect();
    const cx = rect.left + ((area.x + (idx + 0.5) * barW) / canvas.width) * rect.width;
    const bounds = crosshairBounds(canvas.id);
    crosshair.style.display = 'block';
    crosshair.style.left = cx + 'px';
    if (bounds) {
      crosshair.style.top = bounds.top + 'px';
      crosshair.style.height = bounds.height + 'px';
    }
    crosshair.dataset.date = date || points[idx].date;
  }

  function bindChart(canvas, getPoints, tipFn, onClick) {
    canvas.addEventListener('mousemove', e => {
      const points = getPoints();
      if (!points.length) { hideTip(); hoverDate = null; crosshair.style.display = 'none'; render(); return; }
      const area = ca(canvas);
      const rect = canvas.getBoundingClientRect();
      const mx = (e.clientX - rect.left) * (canvas.width / rect.width);
      const barW = area.w / points.length;
      const idx = Math.floor((mx - area.x) / barW);
      if (idx < 0 || idx >= points.length) {
        hideTip(); hoverDate = null; crosshair.style.display = 'none'; render(); return;
      }
      hoverDate = points[idx].date;
      showTip(e, tipFn(points[idx]));
      setCrosshair(canvas, idx, points, hoverDate);
      render();
    });
    canvas.addEventListener('mouseleave', () => {
      hideTip(); hoverDate = null; crosshair.style.display = 'none'; render();
    });
    if (onClick) {
      canvas.addEventListener('click', e => {
        const points = getPoints();
        const area = ca(canvas);
        const rect = canvas.getBoundingClientRect();
        const mx = (e.clientX - rect.left) * (canvas.width / rect.width);
        const idx = Math.floor((mx - area.x) / (area.w / Math.max(points.length, 1)));
        if (idx >= 0 && idx < points.length) onClick(points[idx]);
      });
    }
  }

  function openDrawer(p) {
    const data = filtered();
    const depth = data.depth.find(d => d.date === p.date);
    const promote = data.promotes.find(d => d.date === p.date);
    const dream = data.daydream.find(d => d.date === p.date);

    document.getElementById('drawer-date').textContent = p.date;
    const body = document.getElementById('drawer-body');

    const typeRows = TYPE_ORDER.map(t => {
      const v = (p.counts || {})[t] || 0;
      if (!v) return '';
      return `<div class="drawer-stat"><span>${t}</span><span class="val" style="color:${typeColor(t)}">${v}</span></div>`;
    }).join('');

    const depthRows = depth ? TYPE_ORDER.map(t => {
      const v = (depth.by_type || {})[t];
      if (!v) return '';
      return `<div class="drawer-stat"><span>${t}</span><span class="val">${v}</span></div>`;
    }).join('') : '';

    body.innerHTML =
      `<div class="drawer-section"><h3>Retrieval</h3>` +
      `<div class="drawer-stat"><span>Total recalls</span><span class="val">${p.total}</span></div>` +
      `<div class="drawer-stat"><span>Prompts</span><span class="val">${p.prompts}</span></div>` +
      `<div class="drawer-stat"><span>Avg relevance</span><span class="val" style="color:#f59e0b">${(p.avg_relevance || 0).toFixed(3)}</span></div>` +
      `<div class="drawer-stat"><span>Avg body hits</span><span class="val" style="color:#60a5fa">${(p.avg_body_hits || 0).toFixed(3)}</span></div>` +
      typeRows +
      `</div>` +
      `<div class="drawer-section"><h3>Vault snapshot</h3>` +
      (depth
        ? `<div class="drawer-stat"><span>Total memories</span><span class="val">${depth.total}</span></div>${depthRows}`
        : '<div style="color:var(--muted);font-size:12px">No snapshot</div>') +
      `</div>` +
      `<div class="drawer-section"><h3>Activity</h3>` +
      `<div class="drawer-stat"><span>Promotions</span><span class="val" style="color:#34d399">${promote ? promote.count : 0}</span></div>` +
      `<div class="drawer-stat"><span>Daydream fires</span><span class="val" style="color:#c084fc">${dream ? dream.count : 0}</span></div>` +
      `</div>`;

    document.getElementById('drawer-backdrop').classList.add('open');
    document.getElementById('drawer').classList.add('open');
  }

  function closeDrawer() {
    document.getElementById('drawer-backdrop').classList.remove('open');
    document.getElementById('drawer').classList.remove('open');
  }

  const CANVASES = ['chart-hero', 'chart-adherence', 'chart-vault', 'chart-promotes', 'chart-daydream'];

  function resizeAll() {
    CANVASES.forEach(id => {
      const c = document.getElementById(id);
      if (!c || !c.parentElement) return;
      const w = c.parentElement.clientWidth;
      if (w > 0) c.width = w;
    });
  }

  function render() {
    const data = filtered();
    updateKPIs(data);
    updateVaultCallout(data);
    updateModelTable(data);
    updateAdherenceCallout(data);
    buildAdherenceLegend();

    const recallHi = idxByDate(data.recall, hoverDate);
    const adherenceHi = idxByDate(adherencePoints(data), hoverDate);
    const depthHi = idxByDate(data.depth, hoverDate);
    const promoteHi = idxByDate(data.promotes, hoverDate);
    const dreamHi = idxByDate(data.daydream, hoverDate);

    const hero = document.getElementById('chart-hero');
    if (hero) drawHero(hero, data, recallHi);

    const adherence = document.getElementById('chart-adherence');
    if (adherence) drawAdherence(adherence, data, adherenceHi);

    const vault = document.getElementById('chart-vault');
    if (vault) drawVault(vault, data, depthHi);

    const prom = document.getElementById('chart-promotes');
    if (prom) drawBars(prom, data.promotes, '#34d399', promoteHi);

    const dream = document.getElementById('chart-daydream');
    if (dream) drawBars(dream, data.daydream, '#c084fc', dreamHi);
  }

  function setLiveStatus(state) {
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

    es.addEventListener('open', () => setLiveStatus('connected'));

    es.addEventListener('message', e => {
      try {
        liveData = JSON.parse(e.data);
        resizeAll();
        buildLegend();
        render();
        updateTimestamp(liveData.meta && liveData.meta.generated_at);
        setLiveStatus('connected');
      } catch (err) {
        console.error('[dashboard] SSE parse error:', err);
      }
    });

    es.addEventListener('error', () => {
      setLiveStatus(es.readyState === EventSource.CONNECTING ? 'stale' : 'error');
    });
  }

  function bindClick(id, handler) {
    const el = document.getElementById(id);
    if (el) el.addEventListener('click', handler);
  }

  function showDataError() {
    const main = document.getElementById('cockpit');
    if (!main || main.querySelector('.dashboard-error')) return;
    const note = document.createElement('p');
    note.className = 'dashboard-error';
    note.style.cssText = 'color:var(--warn);font-size:13px;padding:12px 18px;';
    note.textContent = 'Metrics data missing or corrupt — re-run: jm metrics dashboard';
    main.prepend(note);
  }

  window.addEventListener('load', () => {
    if (!liveData || !liveData.recall_by_day) {
      showDataError();
    }

    updateTimestamp(liveData.meta && liveData.meta.generated_at);

    const graphLink = document.getElementById('graph-link');
    if (graphLink && liveData.graph_rel_path) graphLink.href = liveData.graph_rel_path;

    resizeAll();
    buildLegend();

    const hero = document.getElementById('chart-hero');
    if (hero) {
      bindChart(hero, () => filtered().recall, recallTip, p => openDrawer(p));
    }
    const adherence = document.getElementById('chart-adherence');
    if (adherence) {
      bindChart(adherence, () => adherencePoints(filtered()), p => {
        const d = filtered();
        const u = d.usage.find(x => x.date === p.date) || {};
        const rate = p.injected > 0 ? (p.referenced / p.injected * 100).toFixed(0) : '—';
        const src = (u.backfill_turns || 0) > 0 && (u.live_turns || 0) === 0 ? 'backfill'
          : (u.live_turns || 0) > 0 && (u.backfill_turns || 0) === 0 ? 'live' : 'mixed';
        const scope = usageLiveOnly ? 'live' : src;
        return `<div class="tip-date">${p.date}</div>` +
          `<div class="tip-row"><span>injected</span><b>${p.injected}</b></div>` +
          `<div class="tip-row"><span>referenced</span><b>${p.referenced}</b></div>` +
          `<div class="tip-row"><span>usage rate</span><b>${rate}%</b></div>` +
          `<div class="tip-row"><span>source</span><b>${scope}</b> (${u.live_turns || 0} live / ${u.backfill_turns || 0} backfill)</div>` +
          `<div class="tip-row"><span>zero-recall prompts</span><b>${p.zero_recall_prompts}</b></div>`;
      });
    }
    const vault = document.getElementById('chart-vault');
    if (vault) {
      bindChart(vault, () => filtered().depth, p => {
        const rows = Object.entries(p.by_type || {})
          .sort((a, b) => b[1] - a[1])
          .map(([k, v]) => `<div class="tip-row"><span>${k}</span><b>${v}</b></div>`)
          .join('');
        return `<div class="tip-date">${p.date}</div><div class="tip-row"><span>total</span><b>${p.total}</b></div>${rows}`;
      });
    }
    const prom = document.getElementById('chart-promotes');
    if (prom) {
      bindChart(prom, () => filtered().promotes, p => `<div class="tip-date">${p.date}</div>promotions <b>${p.count}</b>`);
    }
    const dream = document.getElementById('chart-daydream');
    if (dream) {
      bindChart(dream, () => filtered().daydream, p => `<div class="tip-date">${p.date}</div>daydreams <b>${p.count}</b>`);
    }

    document.querySelectorAll('.range-btn').forEach(btn => {
      btn.addEventListener('click', () => {
        document.querySelectorAll('.range-btn').forEach(b => b.classList.remove('active'));
        btn.classList.add('active');
        rangeDays = parseInt(btn.dataset.days, 10);
        hoverDate = null;
        render();
      });
    });

    bindClick('smooth-toggle', function () {
      smoothLines = !smoothLines;
      this.classList.toggle('active', smoothLines);
      this.textContent = smoothLines ? '7d avg lines' : 'raw lines';
      render();
    });

    bindClick('usage-live-only-toggle', function () {
      usageLiveOnly = !usageLiveOnly;
      this.classList.toggle('active', usageLiveOnly);
      render();
    });

    bindClick('info-btn', () => {
      alert(
        'Injection (recall log): memories loaded by the hook on each prompt — model-independent.\n' +
        'Zero-recall prompts: retrieval returned nothing (LJM/scoring/topic signal).\n\n' +
        'Usage (memory_usage_log): whether the assistant turn referenced injected memories — model-dependent.\n' +
        'Matched via wiki-links and slug mentions in assistant text.\n' +
        'Backfill rows (faded bars) come from jm metrics backfill-usage replaying transcripts.\n' +
        'Live only (default) hides backfill from usage KPI, adherence chart, and model table.\n' +
        'Injection-only gap: recall logged before usage telemetry existed.\n\n' +
        'Diagnostic: injection healthy + usage dropping = model adherence issue.\n' +
        'Both dropping = LJM retrieval/scoring issue.\n\n' +
        'Body hit depth: avg prompt-keyword hits per recalled memory.\n' +
        'Relevance: scoring-layer match quality (0–1 scale).'
      );
    });

    bindClick('drawer-close', closeDrawer);
    bindClick('drawer-backdrop', closeDrawer);

    render();

    if (window.__DASHBOARD_MODE__ === 'live') connectSSE();
  });

  let resizeTimer;
  window.addEventListener('resize', () => {
    clearTimeout(resizeTimer);
    resizeTimer = setTimeout(() => { resizeAll(); render(); }, 120);
  });
})();