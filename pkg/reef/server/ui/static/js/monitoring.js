// monitoring.js — Monitoring page (logs + metrics)
'use strict';

var ReefMonitoring = (function() {
    var logSSE = null;
    var logLevel = '';

    function render(container) {
        container.innerHTML = '' +
            '<div style="display:grid;grid-template-columns:1fr 1fr;gap:16px;">' +
                '<div class="card">' +
                    '<div class="card-header">' +
                        '<h2>System Logs</h2>' +
                        '<div class="filters">' +
                            '<button class="btn btn-secondary btn-sm log-filter active" data-level="">All</button>' +
                            '<button class="btn btn-secondary btn-sm log-filter" data-level="INFO">INFO</button>' +
                            '<button class="btn btn-secondary btn-sm log-filter" data-level="WARN">WARN</button>' +
                            '<button class="btn btn-secondary btn-sm log-filter" data-level="ERROR">ERROR</button>' +
                        '</div>' +
                    '</div>' +
                    '<div id="log-stream" style="max-height:500px;overflow-y:auto;font-family:monospace;font-size:11px;line-height:1.6;background:var(--bg-primary);padding:12px;border-radius:var(--radius);">' +
                        '<div style="color:var(--text-muted);">Connecting to log stream...</div>' +
                    '</div>' +
                '</div>' +
                '<div>' +
                    '<div class="card"><h2>Task Throughput</h2><canvas id="mon-chart-throughput" height="180"></canvas></div>' +
                    '<div class="card"><h2>Dispatch Latency</h2><canvas id="mon-chart-latency" height="180"></canvas></div>' +
                    '<div class="card"><h2>Queue Depth</h2><canvas id="mon-chart-queue" height="180"></canvas></div>' +
                '</div>' +
            '</div>';

        document.querySelectorAll('.log-filter').forEach(function(btn) {
            btn.addEventListener('click', function() {
                document.querySelectorAll('.log-filter').forEach(function(b) { b.classList.remove('active'); });
                btn.classList.add('active');
                logLevel = btn.getAttribute('data-level');
                reconnectLogSSE();
            });
        });

        connectLogSSE();
    }

    function connectLogSSE() {
        if (logSSE) logSSE.close();
        var url = '/api/v2/logs';
        if (logLevel) url += '?level=' + logLevel;
        logSSE = new EventSource(url);
        var stream = document.getElementById('log-stream');
        if (stream) stream.innerHTML = '';

        logSSE.onmessage = function(e) {
            if (!stream) return;
            try {
                var data = JSON.parse(e.data);
                var color = 'var(--text-secondary)';
                if (data.level === 'ERROR') color = 'var(--error)';
                else if (data.level === 'WARN') color = 'var(--warning)';
                var line = document.createElement('div');
                line.style.color = color;
                line.textContent = '[' + ReefUtils.formatTime(data.timestamp) + '] [' + (data.level||'INFO') + '] ' + (data.message || '');
                stream.appendChild(line);
                stream.scrollTop = stream.scrollHeight;
                // Keep max 500 lines
                while (stream.children.length > 500) stream.removeChild(stream.firstChild);
            } catch (err) {}
        };

        logSSE.onerror = function() {
            if (stream) stream.innerHTML += '<div style="color:var(--warning);">Connection lost, reconnecting...</div>';
            logSSE.close();
            setTimeout(connectLogSSE, 3000);
        };
    }

    function reconnectLogSSE() {
        connectLogSSE();
    }

    return { render: render };
})();
