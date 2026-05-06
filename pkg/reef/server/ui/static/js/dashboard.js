// dashboard.js — Dashboard page
'use strict';

var ReefDashboard = (function() {
    var charts = {};
    var statsData = null;

    function render(container) {
        container.innerHTML = '' +
            '<div class="stats-grid" id="dashboard-stats">' +
                '<div class="card stat-card"><div class="stat-value" id="stat-clients">--</div><div class="stat-label">Connected Clients</div></div>' +
                '<div class="card stat-card"><div class="stat-value" id="stat-completed">--</div><div class="stat-label">Completed Tasks</div></div>' +
                '<div class="card stat-card"><div class="stat-value" id="stat-queue">--</div><div class="stat-label">Queue Depth</div></div>' +
                '<div class="card stat-card"><div class="stat-value" id="stat-uptime">--</div><div class="stat-label">Uptime</div></div>' +
                '<div class="card stat-card"><div class="stat-value" id="stat-version">--</div><div class="stat-label">Version</div></div>' +
            '</div>' +
            '<div style="display:grid;grid-template-columns:1fr 1fr;gap:16px;">' +
                '<div class="card"><h3>Task Throughput</h3><canvas id="chart-throughput" height="200"></canvas></div>' +
                '<div class="card"><h3>Client Load</h3><canvas id="chart-load" height="200"></canvas></div>' +
            '</div>' +
            '<div style="display:grid;grid-template-columns:1fr 1fr;gap:16px;margin-top:16px;">' +
                '<div class="card"><h3>Task Status Distribution</h3><canvas id="chart-status" height="200"></canvas></div>' +
                '<div class="card">' +
                    '<h3>Evolution Status</h3>' +
                    '<div id="evo-status" style="padding:12px 0;font-size:13px;color:var(--text-secondary);">Loading...</div>' +
                '</div>' +
            '</div>' +
            '<div class="card" style="margin-top:16px;">' +
                '<h3>Recent Tasks</h3>' +
                '<div id="recent-tasks" style="margin-top:12px;"></div>' +
            '</div>';

        loadStatus();
        loadRecentTasks();
        loadEvolutionStatus();
    }

    function loadStatus() {
        ReefUtils.apiGet('/api/v2/status').then(function(data) {
            updateStats(data);
        }).catch(function() {});
    }

    function updateStats(data) {
        statsData = data;
        ReefUtils.setText('stat-clients', data.connected_clients || 0);
        ReefUtils.setText('stat-completed', (data.task_stats && data.task_stats.completed) || 0);
        ReefUtils.setText('stat-queue', data.queue_depth || 0);
        ReefUtils.setText('stat-uptime', ReefUtils.formatDuration(data.uptime_ms));
        ReefUtils.setText('stat-version', data.server_version || '--');

        updateCharts(data);
    }

    function updateCharts(data) {
        if (!data.task_stats) return;

        // Status pie chart
        var statusCanvas = document.getElementById('chart-status');
        if (statusCanvas && typeof Chart !== 'undefined') {
            if (charts.status) charts.status.destroy();
            var ts = data.task_stats;
            charts.status = new Chart(statusCanvas.getContext('2d'), {
                type: 'doughnut',
                data: {
                    labels: ['Queued', 'Running', 'Completed', 'Failed', 'Escalated'],
                    datasets: [{
                        data: [ts.queued||0, ts.running||0, ts.completed||0, ts.failed||0, ts.escalated||0],
                        backgroundColor: ['#ff9800', '#2196f3', '#4caf50', '#f44336', '#e94560']
                    }]
                },
                options: { responsive: true, plugins: { legend: { position: 'bottom', labels: { color: '#a0a0a0', font: { size: 11 } } } } }
            });
        }

        // Throughput line chart (placeholder data)
        var throughputCanvas = document.getElementById('chart-throughput');
        if (throughputCanvas && typeof Chart !== 'undefined') {
            if (charts.throughput) charts.throughput.destroy();
            var labels = [];
            var values = [];
            for (var i = 59; i >= 0; i--) {
                labels.push(i + 'm ago');
                values.push(0);
            }
            charts.throughput = new Chart(throughputCanvas.getContext('2d'), {
                type: 'line',
                data: {
                    labels: labels,
                    datasets: [{ label: 'Tasks/min', data: values, borderColor: '#e94560', backgroundColor: 'rgba(233,69,96,0.1)', fill: true, tension: 0.4, pointRadius: 0 }]
                },
                options: { responsive: true, scales: { x: { display: false }, y: { beginAtZero: true, ticks: { color: '#a0a0a0' } } }, plugins: { legend: { display: false } } }
            });
        }

        // Client load bar chart (placeholder)
        var loadCanvas = document.getElementById('chart-load');
        if (loadCanvas && typeof Chart !== 'undefined') {
            if (charts.load) charts.load.destroy();
            charts.load = new Chart(loadCanvas.getContext('2d'), {
                type: 'bar',
                data: {
                    labels: ['No clients'],
                    datasets: [{ label: 'Load', data: [0], backgroundColor: '#2196f3' }, { label: 'Capacity', data: [0], backgroundColor: '#0f3460' }]
                },
                options: { responsive: true, scales: { x: { ticks: { color: '#a0a0a0' } }, y: { beginAtZero: true, ticks: { color: '#a0a0a0' } } }, plugins: { legend: { labels: { color: '#a0a0a0' } } } }
            });
        }
    }

    function loadRecentTasks() {
        ReefUtils.apiGet('/api/v2/tasks?limit=5').then(function(data) {
            var container = document.getElementById('recent-tasks');
            if (!container) return;
            if (!data.tasks || data.tasks.length === 0) {
                container.innerHTML = '<div style="color:var(--text-muted);font-size:13px;">No tasks yet</div>';
                return;
            }
            var html = '<div class="table-container"><table class="data-table"><thead><tr>' +
                '<th>ID</th><th>Status</th><th>Instruction</th><th>Created</th></tr></thead><tbody>';
            data.tasks.forEach(function(t) {
                html += '<tr><td class="mono">' + ReefUtils.escapeHtml(t.id) + '</td>' +
                    '<td>' + ReefUtils.statusBadge(t.status) + '</td>' +
                    '<td>' + ReefUtils.escapeHtml(ReefUtils.truncate(t.instruction, 60)) + '</td>' +
                    '<td>' + ReefUtils.formatTime(t.created_at) + '</td></tr>';
            });
            html += '</tbody></table></div>';
            container.innerHTML = html;
        }).catch(function() {});
    }

    function loadEvolutionStatus() {
        var el = document.getElementById('evo-status');
        if (!el) return;
        ReefUtils.apiGet('/api/v2/evolution/strategy').then(function(data) {
            el.innerHTML = '<div>Strategy: <strong>' + ReefUtils.escapeHtml(data.strategy || 'balanced') + '</strong></div>' +
                '<div style="margin-top:6px;">Innovate: ' + (data.innovate||0) + '% · Optimize: ' + (data.optimize||0) + '% · Repair: ' + (data.repair||0) + '%</div>';
        }).catch(function() {
            el.textContent = 'Evolution not configured';
        });
    }

    return { render: render, updateStats: updateStats };
})();
