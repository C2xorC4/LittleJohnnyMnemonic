(function () {
  'use strict';

  // ---------- Constants ----------
  const TYPE_COLORS = {
    user:      '#f59e0b',
    feedback:  '#84cc16',
    project:   '#3b82f6',
    reference: '#14b8a6',
    semantic:  '#a855f7',
    episodic:  '#ec4899',
    knowledge: '#ffffff',
    buffer:    '#94a3b8',
  };

  const EDGE_HOVER_COLOR = '#a855f7';

  // ---------- Data ----------
  const data = (typeof GRAPH_DATA !== 'undefined') ? GRAPH_DATA : { meta: {}, nodes: [], edges: [] };
  const allNodes = data.nodes || [];
  const allEdges = data.edges || [];

  // ---------- Helpers ----------
  const $ = (id) => document.getElementById(id);

  function hashHue(s) {
    let h = 0;
    for (let i = 0; i < s.length; i++) {
      h = ((h << 5) - h + s.charCodeAt(i)) | 0;
    }
    return Math.abs(h) % 360;
  }

  function tagRingColor(tag) {
    if (!tag) return null;
    return 'hsl(' + hashHue(tag) + ', 70%, 55%)';
  }

  function truncate(s, n) {
    if (!s) return '';
    return s.length > n ? s.slice(0, n - 1) + '…' : s;
  }

  function percentileRanks(values) {
    if (values.length === 0) return new Map();
    const sorted = values.slice().sort((a, b) => a - b);
    const ranks = new Map();
    for (let i = 0; i < sorted.length; i++) {
      const v = sorted[i];
      if (!ranks.has(v)) ranks.set(v, i);
    }
    const n = sorted.length;
    const out = new Map();
    for (const [v, idx] of ranks) {
      out.set(v, n === 1 ? 1.0 : idx / (n - 1));
    }
    return out;
  }

  function nodeSize(activationNorm, degree) {
    const dNorm = Math.min(degree, 20) / 20;
    return 6 + 14 * activationNorm + 6 * dNorm;
  }

  function edgeStyle(e) {
    const isCoact = e.kind === 'coactivation';
    const isContradicts = e.relationship === 'contradicts';
    const isLearned = e.relationship === 'learned';

    let baseColor = '#6b7280';
    let baseOpacity = 0.4;
    if (isContradicts) baseColor = '#ef4444';
    if (isCoact) {
      baseColor = '#4b5563';
      baseOpacity = 0.3;
    }

    let dashes = false;
    if (isLearned) dashes = [6, 4];
    else if (isCoact) dashes = [2, 4];

    const baseWidth = isCoact
      ? Math.max(0.4, (e.weight || 0.3) * 1.5)
      : 0.5 + 3 * (e.weight || 0.5);

    return { baseColor, baseOpacity, baseWidth, dashes };
  }

  // ---------- State ----------
  let activeTypes = new Set(Object.keys(TYPE_COLORS));
  let activeTags = new Set();
  let minActivation = -10.0;
  let searchTerm = '';
  let showCoactivation = true;
  let showIsolated = false;
  let showTagRings = true;

  let nodesDataSet = null;
  let edgesDataSet = null;
  let network = null;
  let degreeMap = new Map();

  // ---------- Build vis data from filtered set ----------
  function buildVisData() {
    const filteredNodesByCriteria = allNodes.filter((n) => {
      if (!activeTypes.has(n.type)) return false;
      if (n.activation < minActivation) return false;
      if (activeTags.size > 0) {
        const tags = n.tags || [];
        if (!tags.some((t) => activeTags.has(t))) return false;
      }
      if (searchTerm) {
        const q = searchTerm.toLowerCase();
        if (!(n.title || '').toLowerCase().includes(q) && !(n.path || '').toLowerCase().includes(q)) return false;
      }
      return true;
    });

    const visibleIds = new Set(filteredNodesByCriteria.map((n) => n.id));
    let visibleEdges = allEdges.filter((e) => visibleIds.has(e.source) && visibleIds.has(e.target));
    if (!showCoactivation) {
      visibleEdges = visibleEdges.filter((e) => e.kind !== 'coactivation');
    }

    const degree = new Map();
    for (const e of visibleEdges) {
      degree.set(e.source, (degree.get(e.source) || 0) + 1);
      degree.set(e.target, (degree.get(e.target) || 0) + 1);
    }

    let finalNodes = filteredNodesByCriteria;
    if (!showIsolated) {
      finalNodes = filteredNodesByCriteria.filter((n) => (degree.get(n.id) || 0) > 0);
    }

    const finalIds = new Set(finalNodes.map((n) => n.id));
    visibleEdges = visibleEdges.filter((e) => finalIds.has(e.source) && finalIds.has(e.target));

    const ranks = percentileRanks(finalNodes.map((n) => n.activation));

    const visNodes = finalNodes.map((n) => {
      const actNorm = ranks.get(n.activation) ?? 0;
      const deg = degree.get(n.id) || 0;
      const size = nodeSize(actNorm, deg);
      const fill = TYPE_COLORS[n.type] || '#94a3b8';
      const tags = n.tags || [];
      const ring = (showTagRings && tags.length > 0) ? tagRingColor(tags[0]) : fill;
      return {
        id: n.id,
        label: truncate(n.title || n.id, 30),
        size: size,
        color: {
          background: fill,
          border: ring,
          highlight: { background: fill, border: EDGE_HOVER_COLOR },
        },
        borderWidth: 2,
        borderWidthSelected: 3,
        font: { size: 10, color: '#9ca3af', face: 'sans-serif' },
        title: [n.title, n.path,
                'activation=' + (n.activation || 0).toFixed(2) + ' · degree=' + deg,
                'tags: ' + (tags.join(', ') || '(none)')].join('\n'),
        _defaultColor: fill,
        _defaultRing: ring,
      };
    });

    const visEdges = visibleEdges.map((e, i) => {
      const s = edgeStyle(e);
      return {
        id: e.source + '|' + e.target + '|' + e.relationship + '|' + e.kind + '|' + i,
        from: e.source,
        to: e.target,
        relationship: e.relationship,
        kind: e.kind,
        color: { color: s.baseColor, opacity: s.baseOpacity },
        width: s.baseWidth,
        dashes: s.dashes,
        arrows: e.directed ? { to: { enabled: true, scaleFactor: 0.5 } } : undefined,
        smooth: false,
        _defaultColor: s.baseColor,
        _defaultOpacity: s.baseOpacity,
        _defaultWidth: s.baseWidth,
      };
    });

    return { visNodes, visEdges, degree };
  }

  function rebuild() {
    const { visNodes, visEdges, degree } = buildVisData();
    degreeMap = degree;

    if (!network) {
      nodesDataSet = new vis.DataSet(visNodes);
      edgesDataSet = new vis.DataSet(visEdges);
      network = new vis.Network($('graph'), { nodes: nodesDataSet, edges: edgesDataSet }, networkOptions());
      wireNetworkEvents();
    } else {
      nodesDataSet.clear();
      nodesDataSet.add(visNodes);
      edgesDataSet.clear();
      edgesDataSet.add(visEdges);
    }

    $('meta-summary').textContent = visNodes.length + ' nodes · ' + visEdges.length + ' edges';
    $('visible-count').textContent = visNodes.length;

    $('empty-state').hidden = allNodes.length > 0;
    $('linkless-banner').hidden = !(allNodes.length > 0 && allEdges.length === 0);
  }

  function networkOptions() {
    return {
      physics: {
        enabled: true,
        solver: 'forceAtlas2Based',
        forceAtlas2Based: {
          gravitationalConstant: -80,
          centralGravity: 0.005,
          springLength: 100,
          springConstant: 0.18,
          damping: 0.4,
          avoidOverlap: 0.5,
        },
        stabilization: {
          enabled: true,
          iterations: 1000,
          updateInterval: 50,
          fit: true,
        },
        maxVelocity: 50,
        minVelocity: 0.1,
      },
      interaction: {
        hover: true,
        hoverConnectedEdges: false,
        tooltipDelay: 300,
        zoomView: true,
        dragView: true,
      },
      nodes: { shape: 'dot' },
      edges: {
        color: { inherit: false },
        smooth: false,
      },
    };
  }

  // ---------- Hover highlight ----------
  function highlightFor(nodeId) {
    if (!network) return;
    const connectedEdges = new Set(network.getConnectedEdges(nodeId));
    const connectedNodes = new Set(network.getConnectedNodes(nodeId));
    connectedNodes.add(nodeId);

    const edgeUpdates = edgesDataSet.get().map((e) => {
      if (connectedEdges.has(e.id)) {
        return {
          id: e.id,
          color: { color: EDGE_HOVER_COLOR, opacity: 1.0 },
          width: e._defaultWidth * 1.8,
        };
      }
      return {
        id: e.id,
        color: { color: e._defaultColor, opacity: 0.05 },
        width: e._defaultWidth,
      };
    });
    edgesDataSet.update(edgeUpdates);

    const nodeUpdates = nodesDataSet.get().map((n) => {
      if (connectedNodes.has(n.id)) {
        return { id: n.id, font: { size: 11, color: '#e5e7eb', face: 'sans-serif' } };
      }
      return { id: n.id, font: { size: 10, color: 'rgba(156,163,175,0.25)', face: 'sans-serif' } };
    });
    nodesDataSet.update(nodeUpdates);
  }

  function clearHighlight() {
    if (!network) return;
    const edgeUpdates = edgesDataSet.get().map((e) => ({
      id: e.id,
      color: { color: e._defaultColor, opacity: e._defaultOpacity },
      width: e._defaultWidth,
    }));
    edgesDataSet.update(edgeUpdates);

    const nodeUpdates = nodesDataSet.get().map((n) => ({
      id: n.id,
      font: { size: 10, color: '#9ca3af', face: 'sans-serif' },
    }));
    nodesDataSet.update(nodeUpdates);
  }

  function wireNetworkEvents() {
    network.on('hoverNode', (params) => highlightFor(params.node));
    network.on('blurNode', clearHighlight);
    network.on('click', (params) => {
      if (params.nodes.length > 0) showDetail(params.nodes[0]);
      else hideDetail();
    });
    network.once('stabilizationIterationsDone', () => {
      network.setOptions({ physics: { enabled: false } });
    });
  }

  // ---------- Detail panel ----------
  function showDetail(nodeId) {
    const node = allNodes.find((n) => n.id === nodeId);
    if (!node) return;

    $('detail').hidden = false;
    $('layout').classList.remove('detail-hidden');
    $('detail-title').textContent = node.title || node.id;
    $('detail-path').textContent = node.path || '';

    const fields = [
      ['Type', node.type],
      ['Activation', (node.activation || 0).toFixed(3)],
      ['Confidence', (node.confidence || 0).toFixed(2)],
      ['Access count', node.access_count || 0],
      ['Degree', degreeMap.get(node.id) || 0],
    ];
    if (node.importance) fields.push(['Importance', node.importance]);

    const dl = document.createElement('dl');
    for (const [k, v] of fields) {
      const dt = document.createElement('dt'); dt.textContent = k;
      const dd = document.createElement('dd'); dd.textContent = v;
      dl.appendChild(dt); dl.appendChild(dd);
    }
    $('detail-meta').replaceChildren(dl);

    const tagsDiv = $('detail-tags');
    tagsDiv.replaceChildren();
    for (const t of (node.tags || [])) {
      const span = document.createElement('span');
      span.className = 'chip';
      span.textContent = t;
      span.style.borderColor = tagRingColor(t);
      tagsDiv.appendChild(span);
    }

    const neighbors = [];
    for (const e of allEdges) {
      if (e.source === nodeId) neighbors.push({ id: e.target, rel: e.relationship, kind: e.kind });
      else if (e.target === nodeId) neighbors.push({ id: e.source, rel: e.relationship, kind: e.kind });
    }
    $('detail-neighbor-count').textContent = '(' + neighbors.length + ')';

    const ul = $('detail-neighbors');
    ul.replaceChildren();
    for (const nb of neighbors) {
      const target = allNodes.find((n) => n.id === nb.id);
      if (!target) continue;
      const li = document.createElement('li');
      const titleSpan = document.createElement('span');
      titleSpan.textContent = truncate(target.title || target.id, 28);
      const relSpan = document.createElement('span');
      relSpan.className = 'neighbor-rel';
      relSpan.textContent = nb.kind === 'coactivation' ? 'coactivation' : nb.rel;
      li.appendChild(titleSpan); li.appendChild(relSpan);
      li.addEventListener('click', () => {
        if (network) {
          network.focus(target.id, { scale: 1.2, animation: { duration: 400 } });
          showDetail(target.id);
        }
      });
      ul.appendChild(li);
    }
  }

  function hideDetail() {
    $('detail').hidden = true;
    $('layout').classList.add('detail-hidden');
  }

  // ---------- Sidebar ----------
  function buildTypeFilter() {
    const counts = {};
    for (const n of allNodes) counts[n.type] = (counts[n.type] || 0) + 1;
    const list = $('type-list');
    list.replaceChildren();
    for (const t of Object.keys(TYPE_COLORS)) {
      if (!counts[t]) continue;
      const label = document.createElement('label');
      const input = document.createElement('input');
      input.type = 'checkbox';
      input.checked = true;
      input.dataset.type = t;
      input.addEventListener('change', () => {
        if (input.checked) activeTypes.add(t);
        else activeTypes.delete(t);
        rebuild();
      });
      const swatch = document.createElement('span');
      swatch.className = 'type-swatch';
      swatch.style.background = TYPE_COLORS[t];
      const name = document.createElement('span');
      name.textContent = t;
      const count = document.createElement('span');
      count.className = 'count';
      count.textContent = counts[t];
      label.appendChild(input);
      label.appendChild(swatch);
      label.appendChild(name);
      label.appendChild(count);
      list.appendChild(label);
    }
  }

  let tagSorted = [];
  function buildTagFilter() {
    const counts = {};
    for (const n of allNodes) {
      for (const t of (n.tags || [])) counts[t] = (counts[t] || 0) + 1;
    }
    tagSorted = Object.entries(counts).sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]));
    $('tag-count').textContent = '(' + tagSorted.length + ')';
    renderTagChips('');
    $('tag-search').addEventListener('input', (e) => renderTagChips(e.target.value));
  }

  function renderTagChips(filter) {
    const list = $('tag-list');
    list.replaceChildren();
    const f = filter.toLowerCase();
    const visible = tagSorted.filter(([t]) => !f || t.toLowerCase().includes(f));
    const limit = filter ? visible.length : Math.min(60, visible.length);
    for (let i = 0; i < limit; i++) {
      const [tag, count] = visible[i];
      const chip = document.createElement('span');
      chip.className = 'chip' + (activeTags.has(tag) ? ' active' : '');
      chip.textContent = tag + ' ' + count;
      chip.title = tag;
      chip.style.borderColor = tagRingColor(tag);
      chip.addEventListener('click', () => {
        if (activeTags.has(tag)) activeTags.delete(tag);
        else activeTags.add(tag);
        renderTagChips(filter);
        rebuild();
      });
      list.appendChild(chip);
    }
  }

  function buildLegendTypes() {
    const counts = {};
    for (const n of allNodes) counts[n.type] = (counts[n.type] || 0) + 1;
    const ul = $('legend-types');
    ul.replaceChildren();
    for (const t of Object.keys(TYPE_COLORS)) {
      if (!counts[t]) continue;
      const li = document.createElement('li');
      const sw = document.createElement('span');
      sw.className = 'type-swatch';
      sw.style.background = TYPE_COLORS[t];
      li.appendChild(sw);
      li.appendChild(document.createTextNode(t + ' (' + counts[t] + ')'));
      ul.appendChild(li);
    }
  }

  // ---------- Bind controls ----------
  $('activation-slider').addEventListener('input', (e) => {
    minActivation = parseFloat(e.target.value);
    $('activation-value').textContent = minActivation.toFixed(1);
    rebuild();
  });

  $('show-coactivation').addEventListener('change', (e) => {
    showCoactivation = e.target.checked;
    rebuild();
  });
  $('show-isolated').addEventListener('change', (e) => {
    showIsolated = e.target.checked;
    rebuild();
  });
  $('show-tag-rings').addEventListener('change', (e) => {
    showTagRings = e.target.checked;
    rebuild();
  });

  let searchTimer;
  $('search').addEventListener('input', (e) => {
    clearTimeout(searchTimer);
    const value = e.target.value.trim();
    searchTimer = setTimeout(() => {
      searchTerm = value;
      rebuild();
      if (searchTerm && network && nodesDataSet.length > 0) {
        const first = nodesDataSet.get()[0];
        if (first) network.focus(first.id, { scale: 1.2, animation: { duration: 400 } });
      }
    }, 150);
  });

  $('relayout').addEventListener('click', () => {
    if (!network) return;
    network.setOptions({ physics: { enabled: true } });
    network.stabilize(500);
    network.once('stabilizationIterationsDone', () => {
      network.setOptions({ physics: { enabled: false } });
    });
  });

  $('reset-filters').addEventListener('click', () => {
    activeTypes = new Set(Object.keys(TYPE_COLORS));
    activeTags = new Set();
    minActivation = -10.0;
    searchTerm = '';
    showCoactivation = true;
    showIsolated = false;
    showTagRings = true;
    $('search').value = '';
    $('tag-search').value = '';
    $('activation-slider').value = -10;
    $('activation-value').textContent = '-10.0';
    $('show-coactivation').checked = true;
    $('show-isolated').checked = false;
    $('show-tag-rings').checked = true;
    document.querySelectorAll('#type-list input[type=checkbox]').forEach((cb) => { cb.checked = true; });
    renderTagChips('');
    rebuild();
  });

  $('detail-close').addEventListener('click', hideDetail);

  // ---------- Init ----------
  buildTypeFilter();
  buildTagFilter();
  buildLegendTypes();
  $('layout').classList.add('detail-hidden');
  rebuild();
})();
