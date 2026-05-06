// dashboard.js — Reef Dashboard with live stats, charts, and task feed
'use strict';

var ReefDashboard = (function() {
    'use strict';

    // ---- State ----
    var charts = {};
    var statsData = null;
    var chartJsLoaded = false;
    var chartJsLoading = false;
    var chartJsCallbacks = [];

    // Ring buffer for throughput (60 slots = ~60 minutes of SSE events)
    var throughputBuffer = [];
    var throughputLabels = [];
    var MAX_THROUGHPUT = 60;

    // Ring buffer for queue depth
    var queueDepthBuffer = [];
    var queueDepthLabels = [];
    var MAX_QUEUE_DEPTH = 60;

    // ---- Chart.js lazy-loader ----
    function loadChartJs(cb) {
        if (chartJsLoaded) { cb(); return; }
        chartJsCallbacks.push(cb);
        if (chartJsLoading) return;
        chartJsLoading = true;
        var script = document.createElement('script');
        script.src = '/static/js/lib/chart.min.js';
        script.onload = function() {
            chartJsLoaded = true;
            chartJsLoading = false;
            var fns = chartJsCallbacks.slice();
            chartJsCallbacks = [];
            fns.forEach(function(fn) { fn(); });
        };
        script.onerror = function() {
            chartJsLoading = false;
            console.warn('Chart.js failed to load; charts disabled.');
        };
        document.head.appendChild(script);
    }

    // ---- Render ----
    function render(container) {
        container.innerHTML = buildHTML();
        loadChartJs(function() {
            loadCharts();
        });
        loadAllData();
    }

    function buildHTML() {
        return '' +
            // ── Stats grid: 6 cards ──
            '<div class="stats-grid" id="dashboard-stats">' +
                '<div class="card stat-card">' +
                    '<div class="stat-icon">🖥️</div>' +
                    '<div class="stat-value" id="stat-clients">--</div>' +
                    '<div class="stat-label">Connected Clients</div>' +
                '</div>' +
                '<div class="card stat-card">' +
                    '<div class="stat-icon">✅</div>' +
                    '<div class="stat-value" id="stat-completed">--</div>' +
                    '<div class="stat-label">Completed Tasks</div>' +
                '</div>' +
                '<div class="card stat-card">' +
                    '<div class="stat-icon">📋</div>' +
                    '<div class="stat-value" id="stat-queue">--</div>' +
                    '<div class="stat-label">Queue Depth</div>' +
                '</div>' +
                '<div class="card stat-card">' +
                    '<div class="stat-icon">⚡</div>' +
                    '<div class="stat-value" id="stat-running">--</div>' +
                    '<div class="stat-label">Running Tasks</div>' +
                '</div>' +
                '<div class="card stat-card">' +
                    '<div class="stat-icon">❌</div>' +
                    '<div class="stat-value" id="stat-failed">--</div>' +
                    '<div class="stat-label">Failed Tasks</div>' +
                '</div>' +
                '<div class="card stat-card">' +
                    '<div class="stat-icon">⏱️</div>' +
                    '<div class="stat-value" id="stat-uptime">--</div>' +
                    '<div class="stat-label">Uptime</div>' +
                '</div>' +
            '</div>' +

            // ── Charts row 1: Throughput + Client Load ──
            '<div style="display:grid;grid-template-columns:1fr 1fr;gap:16px;">' +
                '<div class="card">' +
                    '<h3>📈 Task Throughput <span style="font-weight:400;font-size:11px;color:var(--text-muted);">(last ' + MAX_THROUGHPUT + ' events)</span></h3>' +
                    '<div id="chart-throughput-empty" class="chart-empty hidden">No throughput data yet</div>' +
                    '<canvas id="chart-throughput" height="200"></canvas>' +
                '</div>' +
                '<div class="card">' +
                    '<h3>🖥️ Client Load</h3>' +
                    '<div id="chart-load-empty" class="chart-empty hidden">No clients connected</div>' +
                    '<canvas id="chart-load" height="200"></canvas>' +
                '</div>' +
            '</div>' +

            // ── Charts row 2: Status Doughnut + Queue Depth ──
            '<div style="display:grid;grid-template-columns:1fr 1fr;gap:16px;margin-top:16px;">' +
                '<div class="card">' +
                    '<h3>📊 Task Status Distribution</h3>' +
                    '<div id="chart-status-empty" class="chart-empty hidden">No task data available</div>' +
                    '<canvas id="chart-status" height="200"></canvas>' +
                '</div>' +
                '<div class="card">' +
                    '<h3>📉 Queue Depth <span style="font-weight:400;font-size:11px;color:var(--text-muted);">(last ' + MAX_QUEUE_DEPTH + ' events)</span></h3>' +
                    '<div id="chart-queue-empty" class="chart-empty hidden">No queue data yet</div>' +
                    '<canvas id="chart-queue" height="200"></canvas>' +
                '</div>' +
            '</div>' +

            // ── Row 3: Recent Tasks + Evolution Status ──
            '<div style="display:grid;grid-template-columns:1fr 1fr;gap:16px;margin-top:16px;">' +
                '<div class="card">' +
                    '<h3>📝 Recent Tasks</h3>' +
                    '<div id="recent-tasks"></div>' +
                '</div>' +
                '<div class="card">' +
                    '<h3>🧬 Evolution Status</h3>' +
                    '<div id="evo-status">' +
                        '<div class="evo-loading">Loading evolution data...</div>' +
                    '</div>' +
                '</div>' +
            '</div>';
    }

    // ---- Data Loading ----
    function loadAllData() {
        loadStatus();
        loadRecentTasks();
        loadEvolutionStatus();
    }

    function loadStatus() {
        ReefUtils.apiGet('/api/v2/status').then(function(data) {
            updateStats(data);
            updateClientLoadChart();
            updateAllCharts(data);
        }).catch(function() {
            showEmptyStates();
        });
    }

    function loadRecentTasks() {
        ReefUtils.apiGet('/api/v2/tasks?limit=5').then(function(data) {
            renderRecentTasks(data);
        }).catch(function() {});
    }

    function loadEvolutionStatus() {
        var el = document.getElementById('evo-status');
        if (!el) return;
        ReefUtils.apiGet('/api/v2/evolution/strategy').then(function(data) {
            if (!data) {
                el.innerHTML = '<div class="evo-empty">Evolution not configured</div>';
                return;
            }
            renderEvolutionStatus(el, data);
        }).catch(function() {
            el.innerHTML = '<div class="evo-empty">Evolution data unavailable</div>';
        });
    }

    function updateClientLoadChart() {
        ReefUtils.apiGet('/api/v2/clients').then(function(data) {
            renderClientLoadChart(data || []);
        }).catch(function() {});
    }

    // ---- Stats Update (called by SSE) ----
    function updateStats(data) {
        if (!data) return;
        statsData = data;

        // Update stat cards
        ReefUtils.setText('stat-clients', data.connected_clients || 0);
        ReefUtils.setText('stat-completed', (data.task_stats && data.task_stats.completed) || 0);
        ReefUtils.setText('stat-queue', data.queue_depth || 0);
        ReefUtils.setText('stat-running', (data.task_stats && data.task_stats.running) || 0);
        ReefUtils.setText('stat-failed', (data.task_stats && data.task_stats.failed) || 0);
        ReefUtils.setText('stat-uptime', ReefUtils.formatDuration(data.uptime_ms));

        // Ring buffer for throughput
        var completed = (data.task_stats && data.task_stats.completed) || 0;
        recordThroughput(completed);

        // Ring buffer for queue depth
        var queue = data.queue_depth || 0;
        recordQueueDepth(queue);

        // Update all charts
        updateAllCharts(data);
    }

    // ---- Ring Buffers ----
    function recordThroughput(value) {
        var now = ReefUtils.formatTime(new Date().toISOString());
        throughputBuffer.push(value);
        throughputLabels.push(now);
        if (throughputBuffer.length > MAX_THROUGHPUT) {
            throughputBuffer.shift();
            throughputLabels.shift();
        }
        renderThroughputChart();
    }

    function recordQueueDepth(value) {
        var now = ReefUtils.formatTime(new Date().toISOString());
        queueDepthBuffer.push(value);
        queueDepthLabels.push(now);
        if (queueDepthBuffer.length > MAX_QUEUE_DEPTH) {
            queueDepthBuffer.shift();
            queueDepthLabels.shift();
        }
        renderQueueDepthChart();
    }

    // ---- Chart Rendering ----
    function loadCharts() {
        // Load client chart and render any buffered data
        updateClientLoadChart();
        if (statsData) {
            updateAllCharts(statsData);
        }
    }

    function updateAllCharts(data) {
        if (!data || !chartJsLoaded) return;
        renderStatusChart(data);
        renderThroughputChart();
        renderQueueDepthChart();
    }

    function renderStatusChart(data) {
        var canvas = document.getElementById('chart-status');
        var emptyEl = document.getElementById('chart-status-empty');
        if (!canvas) return;

        var ts = data.task_stats;
        if (!ts || ((ts.queued || 0) + (ts.running || 0) + (ts.completed || 0) + (ts.failed || 0) + (ts.escalated || 0)) === 0) {
            canvas.style.display = 'none';
            if (emptyEl) emptyEl.classList.remove('hidden');
            return;
        }
        canvas.style.display = 'block';
        if (emptyEl) emptyEl.classList.add('hidden');

        if (charts.status) charts.status.destroy();
        charts.status = new Chart(canvas.getContext('2d'), {
            type: 'doughnut',
            data: {
                labels: ['Queued', 'Running', 'Completed', 'Failed', 'Escalated'],
                datasets: [{
                    data: [ts.queued || 0, ts.running || 0, ts.completed || 0, ts.failed || 0, ts.escalated || 0],
                    backgroundColor: ['#ff9800', '#2196f3', '#4caf50', '#f44336', '#e94560'],
                    borderWidth: 0
                }]
            },
            options: {
                responsive: true,
                cutout: '60%',
                plugins: {
                    legend: { position: 'bottom', labels: { color: '#a0a0a0', font: { size: 11 }, padding: 12, usePointStyle: true } }
                }
            }
        });
    }

    function renderThroughputChart() {
        var canvas = document.getElementById('chart-throughput');
        var emptyEl = document.getElementById('chart-throughput-empty');
        if (!canvas || !chartJsLoaded) return;

        if (throughputBuffer.length === 0) {
            canvas.style.display = 'none';
            if (emptyEl) emptyEl.classList.remove('hidden');
            return;
        }
        canvas.style.display = 'block';
        if (emptyEl) emptyEl.classList.add('hidden');

        if (charts.throughput) charts.throughput.destroy();

        // Compute deltas (change from previous entry)
        var values = [];
        for (var i = 0; i < throughputBuffer.length; i++) {
            if (i === 0) {
                values.push(0);
            } else {
                values.push(Math.max(0, throughputBuffer[i] - throughputBuffer[i - 1]));
            }
        }

        charts.throughput = new Chart(canvas.getContext('2d'), {
            type: 'line',
            data: {
                labels: throughputLabels,
                datasets: [{
                    label: 'Completed / event',
                    data: values,
                    borderColor: '#4caf50',
                    backgroundColor: 'rgba(76,175,80,0.08)',
                    fill: true,
                    tension: 0.3,
                    pointRadius: 0,
                    borderWidth: 2
                }]
            },
            options: {
                responsive: true,
                scales: {
                    x: { display: false },
                    y: { beginAtZero: true, ticks: { color: '#a0a0a0', font: { size: 10 }, precision: 0 } }
                },
                plugins: { legend: { display: false } }
            }
        });
    }

    function renderQueueDepthChart() {
        var canvas = document.getElementById('chart-queue');
        var emptyEl = document.getElementById('chart-queue-empty');
        if (!canvas || !chartJsLoaded) return;

        if (queueDepthBuffer.length === 0) {
            canvas.style.display = 'none';
            if (emptyEl) emptyEl.classList.remove('hidden');
            return;
        }
        canvas.style.display = 'block';
        if (emptyEl) emptyEl.classList.add('hidden');

        if (charts.queue) charts.queue.destroy();

        charts.queue = new Chart(canvas.getContext('2d'), {
            type: 'line',
            data: {
                labels: queueDepthLabels,
                datasets: [{
                    label: 'Queue Depth',
                    data: queueDepthBuffer,
                    borderColor: '#ff9800',
                    backgroundColor: 'rgba(255,152,0,0.08)',
                    fill: true,
                    tension: 0.3,
                    pointRadius: 0,
                    borderWidth: 2
                }]
            },
            options: {
                responsive: true,
                scales: {
                    x: { display: false },
                    y: { beginAtZero: true, ticks: { color: '#a0a0a0', font: { size: 10 }, precision: 0 } }
                },
                plugins: { legend: { display: false } }
            }
        });
    }

    function renderClientLoadChart(clients) {
        var canvas = document.getElementById('chart-load');
        var emptyEl = document.getElementById('chart-load-empty');
        if (!canvas || !chartJsLoaded) return;

        if (!clients || clients.length === 0) {
            canvas.style.display = 'none';
            if (emptyEl) emptyEl.classList.remove('hidden');
            return;
        }
        canvas.style.display = 'block';
        if (emptyEl) emptyEl.classList.add('hidden');

        if (charts.load) charts.load.destroy();

        var labels = clients.map(function(c) { return ReefUtils.escapeHtml(c.id || '?'); });
        var loadVals = clients.map(function(c) { return c.load || 0; });
        var capVals = clients.map(function(c) { return Math.max(0, (c.capacity || 5) - (c.load || 0)); });

        charts.load = new Chart(canvas.getContext('2d'), {
            type: 'bar',
            data: {
                labels: labels,
                datasets: [
                    { label: 'Load', data: loadVals, backgroundColor: '#e94560', borderRadius: 4 },
                    { label: 'Available', data: capVals, backgroundColor: '#0f3460', borderRadius: 4 }
                ]
            },
            options: {
                responsive: true,
                scales: {
                    x: { stacked: true, ticks: { color: '#a0a0a0', font: { size: 10 }, maxRotation: 45 } },
                    y: { stacked: true, beginAtZero: true, ticks: { color: '#a0a0a0', font: { size: 10 }, precision: 0 } }
                },
                plugins: {
                    legend: { labels: { color: '#a0a0a0', font: { size: 11 }, usePointStyle: true } }
                }
            }
        });
    }

    // ---- Recent Tasks Table ----
    function renderRecentTasks(data) {
        var container = document.getElementById('recent-tasks');
        if (!container) return;

        if (!data || !data.tasks || data.tasks.length === 0) {
            container.innerHTML = '' +
                '<div class="empty-state" style="padding:20px 0;">' +
                    '<div class="empty-icon">📭</div>' +
                    '<div class="empty-text">No tasks yet</div>' +
                    '<div style="font-size:12px;color:var(--text-muted);margin-top:4px;">Submit a task to get started</div>' +
                '</div>';
            return;
        }

        var html = '<div class="table-container"><table class="data-table data-table-clickable"><thead><tr>' +
            '<th>ID</th><th>Status</th><th>Instruction</th><th>Created</th></tr></thead><tbody>';
        data.tasks.forEach(function(t) {
            var taskId = ReefUtils.escapeHtml(t.id || '');
            html += '<tr class="clickable-row" data-task-id="' + taskId + '" onclick="ReefDashboard.navigateToChatroom(\'' + taskId + '\')" title="Open in chatroom">' +
                '<td class="mono">' + taskId + '</td>' +
                '<td>' + ReefUtils.statusBadge(t.status) + '</td>' +
                '<td>' + ReefUtils.escapeHtml(ReefUtils.truncate(t.instruction, 80)) + '</td>' +
                '<td>' + ReefUtils.formatTime(t.created_at) + '</td>' +
            '</tr>';
        });
        html += '</tbody></table></div>';
        container.innerHTML = html;
    }

    function navigateToChatroom(taskId) {
        if (!taskId) return;
        ReefApp.go('/chatroom?task=' + encodeURIComponent(taskId));
    }

    // ---- Evolution Status ----
    function renderEvolutionStatus(el, data) {
        var strategy = data.strategy || 'balanced';
        var innovate = data.innovate != null ? data.innovate : 0;
        var optimize = data.optimize != null ? data.optimize : 0;
        var repair = data.repair != null ? data.repair : 0;

        var total = innovate + optimize + repair;
        if (total === 0) {
            el.innerHTML = '<div class="evo-empty">Evolution not configured</div>';
            return;
        }

        var innovPct = Math.round((innovate / total) * 100);
        var optPct = Math.round((optimize / total) * 100);
        var repPct = Math.round((repair / total) * 100);

        el.innerHTML = '' +
            '<div class="evo-strategy">' +
                '<span class="evo-label">Strategy:</span>' +
                '<span class="evo-value">' + ReefUtils.escapeHtml(strategy) + '</span>' +
            '</div>' +
            '<div class="evo-bars" style="margin-top:12px;">' +
                '<div class="evo-bar-row">' +
                    '<span class="evo-bar-label">Innovate</span>' +
                    '<div class="evo-bar-track"><div class="evo-bar-fill evo-bar-innovate" style="width:' + innovPct + '%"></div></div>' +
                    '<span class="evo-bar-pct">' + innovPct + '%</span>' +
                '</div>' +
                '<div class="evo-bar-row">' +
                    '<span class="evo-bar-label">Optimize</span>' +
                    '<div class="evo-bar-track"><div class="evo-bar-fill evo-bar-optimize" style="width:' + optPct + '%"></div></div>' +
                    '<span class="evo-bar-pct">' + optPct + '%</span>' +
                '</div>' +
                '<div class="evo-bar-row">' +
                    '<span class="evo-bar-label">Repair</span>' +
                    '<div class="evo-bar-track"><div class="evo-bar-fill evo-bar-repair" style="width:' + repPct + '%"></div></div>' +
                    '<span class="evo-bar-pct">' + repPct + '%</span>' +
                '</div>' +
            '</div>' +
            '<div class="evo-raw" style="margin-top:8px;font-size:11px;color:var(--text-muted);">' +
                'Raw: ' + innovate + ' / ' + optimize + ' / ' + repair +
            '</div>';
    }

    // ---- Empty States ----
    function showEmptyStates() {
        // Called when /api/v2/status fails entirely
        ['stat-clients', 'stat-completed', 'stat-queue', 'stat-running', 'stat-failed', 'stat-uptime'].forEach(function(id) {
            ReefUtils.setText(id, '--');
        });
        var tasksEl = document.getElementById('recent-tasks');
        if (tasksEl) {
            tasksEl.innerHTML = '' +
                '<div class="empty-state" style="padding:20px 0;">' +
                    '<div class="empty-icon">🔌</div>' +
                    '<div class="empty-text">Unable to reach server</div>' +
                    '<div style="font-size:12px;color:var(--text-muted);margin-top:4px;">Check your connection and try again</div>' +
                '</div>';
        }
    }

    // ---- Public API ----
    return {
        render: render,
        updateStats: updateStats,
        navigateToChatroom: navigateToChatroom
    };
})();
