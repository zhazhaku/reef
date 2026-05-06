// clients.js — Clients page with card/table views, detail panel, SSE streaming, filters
'use strict';

var ReefClients = (function() {
    var viewMode = 'card'; // 'card' or 'table'
    var clientsData = [];
    var currentDetailId = null;
    var sessionEventSource = null;
    var detailRefreshTimer = null;

    // Avatar color palette
    var AVATAR_COLORS = [
        '#e94560', '#2196f3', '#4caf50', '#ff9800', '#9c27b0',
        '#00bcd4', '#8bc34a', '#ff5722', '#3f51b5', '#009688',
        '#e91e63', '#03a9f4', '#cddc39', '#ffc107', '#795548'
    ];

    function getAvatarColor(name) {
        if (!name) return AVATAR_COLORS[0];
        var idx = 0;
        for (var i = 0; i < name.length; i++) { idx = (idx * 31 + name.charCodeAt(i)) % AVATAR_COLORS.length; }
        return AVATAR_COLORS[idx];
    }

    // ---- Format heartbeat relative time ----
    function formatHeartbeat(ts) {
        if (!ts) return 'offline';
        try {
            var d = new Date(ts);
            if (isNaN(d.getTime())) return 'offline';
            var now = Date.now();
            var diff = now - d.getTime();
            if (diff < 0) return 'just now';
            var sec = Math.floor(diff / 1000);
            if (sec < 10) return 'just now';
            if (sec < 60) return sec + 's ago';
            var min = Math.floor(sec / 60);
            if (min < 60) return min + 'm ago';
            var hr = Math.floor(min / 60);
            if (hr < 24) return hr + 'h ago';
            var days = Math.floor(hr / 24);
            return days + 'd ago';
        } catch (e) {
            return '--';
        }
    }

    function formatHeartbeatFull(ts) {
        if (!ts) return 'Never';
        try {
            var d = new Date(ts);
            if (isNaN(d.getTime())) return 'Never';
            return d.toLocaleString();
        } catch (e) {
            return '--';
        }
    }

    // ---- Render list view ----
    function render(container) {
        currentDetailId = null;
        cleanupDetail();

        container.innerHTML =
            '<div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:16px;flex-wrap:wrap;gap:8px;">' +
                '<div class="filters" style="display:flex;gap:8px;align-items:center;flex-wrap:wrap;">' +
                    '<select id="client-filter-state" class="filter-select">' +
                        '<option value="">All States</option>' +
                        '<option value="online">🟢 Online</option>' +
                        '<option value="offline">🔴 Offline</option>' +
                        '<option value="stale">⚠️ Stale</option>' +
                    '</select>' +
                    '<input type="text" id="client-filter-search" class="filter-input" placeholder="Search by ID or role..." style="min-width:180px;">' +
                '</div>' +
                '<div style="display:flex;gap:8px;">' +
                    '<button id="view-card" class="btn btn-secondary btn-sm" style="' + (viewMode === 'card' ? 'background:var(--accent);color:white;border-color:var(--accent);' : '') + '">📇 Cards</button>' +
                    '<button id="view-table" class="btn btn-secondary btn-sm" style="' + (viewMode === 'table' ? 'background:var(--accent);color:white;border-color:var(--accent);' : '') + '">📊 Table</button>' +
                '</div>' +
            '</div>' +
            '<div id="clients-container"></div>';

        document.getElementById('client-filter-state').addEventListener('change', renderClients);
        document.getElementById('client-filter-search').addEventListener('input', debounce(renderClients, 300));
        document.getElementById('view-card').addEventListener('click', function() { setView('card'); });
        document.getElementById('view-table').addEventListener('click', function() { setView('table'); });

        refresh();
    }

    function refresh() {
        ReefUtils.apiGet('/api/v2/clients').then(function(data) {
            clientsData = Array.isArray(data) ? data : (data.clients || []);
            renderClients();
        }).catch(function() {
            renderClients();
        });
    }

    function renderClients() {
        var container = document.getElementById('clients-container');
        if (!container) return;

        var stateFilter = (document.getElementById('client-filter-state') || {}).value || '';
        var searchFilter = ((document.getElementById('client-filter-search') || {}).value || '').toLowerCase().trim();

        var filtered = clientsData.filter(function(c) {
            // State filter
            if (stateFilter) {
                var s = (c.state || '').toLowerCase();
                if (stateFilter === 'online' && s !== 'connected' && s !== 'online') return false;
                if (stateFilter === 'offline' && s !== 'disconnected' && s !== 'offline') return false;
                if (stateFilter === 'stale' && s !== 'stale') return false;
            }
            // Search filter
            if (searchFilter) {
                var idMatch = (c.id || '').toLowerCase().indexOf(searchFilter) !== -1;
                var roleMatch = (c.role || '').toLowerCase().indexOf(searchFilter) !== -1;
                if (!idMatch && !roleMatch) return false;
            }
            return true;
        });

        if (filtered.length === 0) {
            container.innerHTML =
                '<div class="empty-state">' +
                    '<div class="empty-icon">🤖</div>' +
                    '<div class="empty-text">No clients connected</div>' +
                    '<div style="font-size:12px;color:var(--text-muted);margin-top:8px;">' +
                        (clientsData.length === 0 ? 'No agents are currently registered. Start an agent to see it here.' : 'No agents match the current filters.') +
                    '</div>' +
                '</div>';
            return;
        }

        if (viewMode === 'card') {
            renderCardView(container, filtered);
        } else {
            renderTableView(container, filtered);
        }
    }

    // ---- Card view ----
    function renderCardView(container, clients) {
        var html = '<div class="agent-grid">';
        clients.forEach(function(c) {
            var initials = (c.id || '?')[0].toUpperCase();
            var avatarColor = getAvatarColor(c.id);
            var stateClass = ReefUtils.stateClass(c.state || 'offline');
            var role = c.role || 'agent';
            var skills = ReefUtils.skillBadges(c.skills || []);
            var load = c.load !== undefined ? c.load : (c.current_load !== undefined ? c.current_load : 0);

            html +=
                '<div class="agent-card" data-client-id="' + ReefUtils.escapeHtml(c.id) + '">' +
                    '<div class="agent-card-header">' +
                        '<div class="agent-avatar" style="background:' + avatarColor + ';color:white;">' +
                            initials +
                            '<span class="status-dot ' + stateClass + '"></span>' +
                        '</div>' +
                        '<div style="flex:1;min-width:0;">' +
                            '<div class="agent-card-name">' + ReefUtils.escapeHtml(c.id) + '</div>' +
                            '<div class="agent-card-role">' + ReefUtils.escapeHtml(role) + '</div>' +
                        '</div>' +
                    '</div>' +
                    '<div style="margin-bottom:8px;">' + (skills || '<span style="color:var(--text-muted);font-size:10px;">No skills</span>') + '</div>' +
                    '<div class="agent-card-stats">' +
                        '<span>📊 Load: ' + load + '</span>' +
                        '<span>🕐 ' + formatHeartbeat(c.last_heartbeat) + '</span>' +
                        (c.current_task ? '<span style="color:var(--accent);">📝 Task</span>' : '') +
                    '</div>' +
                '</div>';
        });
        html += '</div>';
        container.innerHTML = html;

        // Click handlers
        container.querySelectorAll('.agent-card').forEach(function(card) {
            card.addEventListener('click', function() {
                var cid = card.getAttribute('data-client-id');
                if (cid) ReefApp.go('/clients/detail?id=' + encodeURIComponent(cid));
            });
        });
    }

    // ---- Table view ----
    function renderTableView(container, clients) {
        var html =
            '<div class="table-container">' +
                '<table class="data-table">' +
                    '<thead><tr>' +
                        '<th>Agent</th><th>Role</th><th>Skills</th><th>State</th><th>Load</th><th>Heartbeat</th><th>Current Task</th>' +
                    '</tr></thead>' +
                    '<tbody>';

        clients.forEach(function(c) {
            var initials = (c.id || '?')[0].toUpperCase();
            var avatarColor = getAvatarColor(c.id);
            var stateClass = ReefUtils.stateClass(c.state || 'offline');
            var stateLabel = (c.state || 'unknown');
            var role = c.role || '--';
            var skills = ReefUtils.skillBadges(c.skills || []);
            var load = c.load !== undefined ? c.load : (c.current_load !== undefined ? c.current_load : 0);
            var task = c.current_task || '--';

            html +=
                '<tr style="cursor:pointer;" data-client-id="' + ReefUtils.escapeHtml(c.id) + '">' +
                    '<td>' +
                        '<div style="display:flex;align-items:center;gap:8px;">' +
                            '<span style="display:inline-flex;width:24px;height:24px;border-radius:50%;background:' + avatarColor +
                                ';color:white;align-items:center;justify-content:center;font-size:10px;font-weight:700;">' + initials + '</span>' +
                            '<span class="mono">' + ReefUtils.escapeHtml(c.id) + '</span>' +
                        '</div>' +
                    '</td>' +
                    '<td>' + ReefUtils.escapeHtml(role) + '</td>' +
                    '<td>' + (skills || '--') + '</td>' +
                    '<td><span class="status-dot ' + stateClass + '" style="margin-right:4px;"></span>' + ReefUtils.escapeHtml(stateLabel) + '</td>' +
                    '<td>' + load + '</td>' +
                    '<td style="font-size:11px;color:var(--text-muted);">' + formatHeartbeat(c.last_heartbeat) + '</td>' +
                    '<td class="mono" style="font-size:11px;">' + ReefUtils.escapeHtml(ReefUtils.truncate(task, 30)) + '</td>' +
                '</tr>';
        });

        html += '</tbody></table></div>';
        container.innerHTML = html;

        // Click handlers
        container.querySelectorAll('tr[data-client-id]').forEach(function(row) {
            row.addEventListener('click', function() {
                var cid = row.getAttribute('data-client-id');
                if (cid) ReefApp.go('/clients/detail?id=' + encodeURIComponent(cid));
            });
        });
    }

    // ---- Client detail view ----
    function renderDetail(container, clientId) {
        currentDetailId = clientId;
        cleanupDetail();

        if (!clientId) { ReefApp.go('/clients'); return; }

        container.innerHTML =
            '<div style="color:var(--text-muted);padding:24px;text-align:center;">' +
                'Loading client <span class="mono">' + ReefUtils.escapeHtml(clientId) + '</span>...' +
            '</div>';

        loadClientDetail(container, clientId);
    }

    function loadClientDetail(container, clientId) {
        ReefUtils.apiGet('/api/v2/client/' + clientId).then(function(client) {
            if (currentDetailId !== clientId) return;

            var initials = (client.id || '?')[0].toUpperCase();
            var avatarColor = getAvatarColor(client.id);
            var stateClass = ReefUtils.stateClass(client.state || 'offline');
            var state = client.state || 'unknown';
            var role = client.role || '--';
            var skills = ReefUtils.skillBadges(client.skills || []);
            var capacity = client.capacity !== undefined ? client.capacity : (client.max_load !== undefined ? client.max_load : '--');
            var currentTask = client.current_task || 'None';
            var lastHeartbeat = formatHeartbeatFull(client.last_heartbeat);

            container.innerHTML =
                // Back button
                '<div style="margin-bottom:16px;">' +
                    '<button class="btn btn-secondary btn-sm" id="client-detail-back">← Back to Clients</button>' +
                '</div>' +

                // Client info card
                '<div class="card" style="margin-bottom:16px;">' +
                    '<div style="display:flex;align-items:flex-start;gap:16px;margin-bottom:16px;">' +
                        '<div class="agent-avatar" style="width:48px;height:48px;font-size:18px;background:' + avatarColor + ';color:white;">' +
                            initials +
                            '<span class="status-dot ' + stateClass + '" style="width:12px;height:12px;"></span>' +
                        '</div>' +
                        '<div style="flex:1;">' +
                            '<h2 style="margin:0 0 4px 0;font-size:18px;">' + ReefUtils.escapeHtml(client.id) + '</h2>' +
                            '<div style="font-size:13px;color:var(--text-secondary);">' +
                                ReefUtils.escapeHtml(role) + ' · ' +
                                '<span class="status-dot ' + stateClass + '" style="margin:0 2px;"></span> ' +
                                ReefUtils.escapeHtml(state) +
                            '</div>' +
                        '</div>' +
                    '</div>' +

                    // Info grid
                    '<div style="display:grid;grid-template-columns:repeat(auto-fill,minmax(160px,1fr));gap:12px;margin-bottom:16px;">' +
                        '<div><div style="font-size:10px;color:var(--text-muted);text-transform:uppercase;letter-spacing:0.5px;">Client ID</div><div class="mono" style="font-size:12px;">' + ReefUtils.escapeHtml(client.id) + '</div></div>' +
                        '<div><div style="font-size:10px;color:var(--text-muted);text-transform:uppercase;letter-spacing:0.5px;">Role</div><div style="font-size:13px;">' + ReefUtils.escapeHtml(role) + '</div></div>' +
                        '<div><div style="font-size:10px;color:var(--text-muted);text-transform:uppercase;letter-spacing:0.5px;">Skills</div><div>' + (skills || '--') + '</div></div>' +
                        '<div><div style="font-size:10px;color:var(--text-muted);text-transform:uppercase;letter-spacing:0.5px;">State</div><div style="font-size:13px;text-transform:capitalize;">' + ReefUtils.escapeHtml(state) + '</div></div>' +
                        '<div><div style="font-size:10px;color:var(--text-muted);text-transform:uppercase;letter-spacing:0.5px;">Capacity</div><div style="font-size:13px;">' + capacity + '</div></div>' +
                        '<div><div style="font-size:10px;color:var(--text-muted);text-transform:uppercase;letter-spacing:0.5px;">Current Task</div><div class="mono" style="font-size:11px;">' + ReefUtils.escapeHtml(ReefUtils.truncate(currentTask, 25)) + '</div></div>' +
                        '<div><div style="font-size:10px;color:var(--text-muted);text-transform:uppercase;letter-spacing:0.5px;">Last Heartbeat</div><div style="font-size:11px;color:var(--text-muted);">' + lastHeartbeat + '</div></div>' +
                    '</div>' +

                    // Action buttons
                    '<div style="display:flex;gap:8px;flex-wrap:wrap;">' +
                        '<button class="btn btn-secondary btn-sm" id="client-action-pause">⏸ Pause</button>' +
                        '<button class="btn btn-success btn-sm" id="client-action-resume">▶ Resume</button>' +
                        '<button class="btn btn-danger btn-sm" id="client-action-restart">🔄 Restart</button>' +
                    '</div>' +
                '</div>' +

                // Live session stream
                '<div class="card">' +
                    '<div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:8px;">' +
                        '<h3 style="font-size:14px;margin:0;">📡 Live Session Stream</h3>' +
                        '<button class="btn btn-secondary btn-sm" id="client-session-clear">Clear</button>' +
                    '</div>' +
                    '<div id="client-session-stream" style="margin-top:8px;max-height:400px;overflow-y:auto;font-family:\'SF Mono\',Consolas,Monaco,monospace;font-size:11px;color:var(--text-secondary);background:var(--bg-primary);border:1px solid var(--border);border-radius:var(--radius-sm);padding:8px;">' +
                        '<div style="color:var(--text-muted);">Connecting to session stream...</div>' +
                    '</div>' +
                '</div>';

            // Wire up back button
            document.getElementById('client-detail-back').addEventListener('click', function() {
                ReefApp.go('/clients');
            });

            // Wire up action buttons
            document.getElementById('client-action-pause').addEventListener('click', function() { pause(client.id); });
            document.getElementById('client-action-resume').addEventListener('click', function() { resume(client.id); });
            document.getElementById('client-action-restart').addEventListener('click', function() { restart(client.id); });

            // Wire up clear button
            document.getElementById('client-session-clear').addEventListener('click', function() {
                var stream = document.getElementById('client-session-stream');
                if (stream) stream.innerHTML = '<div style="color:var(--text-muted);">Cleared. Waiting for events...</div>';
            });

            // Open SSE session stream
            var streamEl = document.getElementById('client-session-stream');
            openSessionStream(client.id, streamEl);

            // Periodic refresh of client detail
            detailRefreshTimer = setInterval(function() {
                if (currentDetailId === clientId) {
                    refreshDetailInfo(clientId);
                }
            }, 10000);

        }).catch(function(err) {
            if (currentDetailId !== clientId) return;
            container.innerHTML =
                '<div style="margin-bottom:16px;">' +
                    '<button class="btn btn-secondary btn-sm" onclick="ReefApp.go(\'/clients\')">← Back to Clients</button>' +
                '</div>' +
                '<div class="empty-state">' +
                    '<div class="empty-icon">❌</div>' +
                    '<div class="empty-text">Client not found</div>' +
                    '<div style="font-size:12px;color:var(--text-muted);margin-top:8px;">' + ReefUtils.escapeHtml(err.message) + '</div>' +
                '</div>';
        });
    }

    function refreshDetailInfo(clientId) {
        if (!clientId || currentDetailId !== clientId) return;
        ReefUtils.apiGet('/api/v2/client/' + clientId).then(function(client) {
            if (currentDetailId !== clientId) return;
            // Only update non-interactive parts: state dot, last heartbeat, current task
            var stateClass = ReefUtils.stateClass(client.state || 'offline');
            var heartbeatEl = document.querySelector('#client-session-stream').parentElement;
            // We'll just rely on SSE for live updates; periodic refresh keeps state in sync
        }).catch(function() {});
    }

    // ---- Session SSE stream ----
    function openSessionStream(clientId, targetEl) {
        closeSessionStream();
        if (!clientId || !targetEl) return;

        sessionEventSource = new EventSource('/api/v2/client/' + clientId + '/session');

        sessionEventSource.onopen = function() {
            var line = document.createElement('div');
            line.style.cssText = 'color:var(--success);padding:2px 0;';
            line.textContent = '[Connected to session stream]';
            targetEl.appendChild(line);
            targetEl.scrollTop = targetEl.scrollHeight;
        };

        sessionEventSource.onmessage = function(e) {
            try {
                var data = JSON.parse(e.data);
                appendSessionEvent(targetEl, data);
            } catch (err) {
                var line = document.createElement('div');
                line.style.cssText = 'padding:2px 0;color:var(--text-secondary);';
                line.textContent = e.data;
                targetEl.appendChild(line);
                targetEl.scrollTop = targetEl.scrollHeight;
            }
        };

        // Named events
        var eventTypes = ['task_start', 'task_complete', 'task_error', 'tool_call', 'tool_result',
                          'heartbeat', 'state_change', 'message', 'log', 'error'];

        eventTypes.forEach(function(evtType) {
            sessionEventSource.addEventListener(evtType, function(e) {
                try {
                    var data = JSON.parse(e.data);
                    data._eventType = evtType;
                    appendSessionEvent(targetEl, data);
                } catch (err) {
                    var line = document.createElement('div');
                    line.style.cssText = 'padding:2px 0;';
                    line.textContent = '[' + evtType + '] ' + e.data;
                    targetEl.appendChild(line);
                    targetEl.scrollTop = targetEl.scrollHeight;
                }
            });
        });

        sessionEventSource.onerror = function() {
            var line = document.createElement('div');
            line.style.cssText = 'color:var(--error);padding:2px 0;';
            line.textContent = '[Session stream disconnected — reconnecting...]';
            targetEl.appendChild(line);
            targetEl.scrollTop = targetEl.scrollHeight;
        };
    }

    function appendSessionEvent(targetEl, data) {
        var line = document.createElement('div');
        line.style.cssText = 'padding:2px 0;border-bottom:1px solid var(--border-light);word-break:break-all;';

        var evtType = data._eventType || data.event || data.type || '';
        var ts = data.timestamp ? formatSessionTime(data.timestamp) + ' ' : '';

        // Color-code by event type
        var typeColor = 'var(--accent)';
        if (evtType === 'task_complete' || evtType === 'tool_result') typeColor = 'var(--success)';
        else if (evtType === 'task_error' || evtType === 'error') typeColor = 'var(--error)';
        else if (evtType === 'heartbeat') typeColor = 'var(--text-muted)';
        else if (evtType === 'state_change') typeColor = 'var(--warning)';

        var typeHtml = evtType ? '<span style="color:' + typeColor + ';font-weight:600;">[' + ReefUtils.escapeHtml(evtType) + ']</span> ' : '';
        var msgText = data.message || data.content || data.text || JSON.stringify(data);

        line.innerHTML = ts + typeHtml + ReefUtils.escapeHtml(typeof msgText === 'string' ? msgText : JSON.stringify(msgText));

        targetEl.appendChild(line);
        targetEl.scrollTop = targetEl.scrollHeight;

        // Limit lines to prevent memory issues
        while (targetEl.children.length > 500) {
            targetEl.removeChild(targetEl.firstChild);
        }
    }

    function formatSessionTime(ts) {
        if (!ts) return '';
        try {
            var d = new Date(ts);
            if (isNaN(d.getTime())) return '';
            var h = d.getHours(), m = d.getMinutes(), s = d.getSeconds();
            return (h < 10 ? '0' : '') + h + ':' + (m < 10 ? '0' : '') + m + ':' + (s < 10 ? '0' : '') + s;
        } catch (e) { return ''; }
    }

    function closeSessionStream() {
        if (sessionEventSource) {
            sessionEventSource.close();
            sessionEventSource = null;
        }
    }

    // ---- Client actions ----
    function pause(id) {
        ReefUtils.apiPost('/api/v2/client/' + id + '/pause').then(function() {
            ReefUtils.toast('Client ' + id + ' paused', 'info');
            refresh();
            if (currentDetailId === id) refreshDetailInfo(id);
        }).catch(function(e) { ReefUtils.toast('Pause failed: ' + e.message, 'error'); });
    }

    function resume(id) {
        ReefUtils.apiPost('/api/v2/client/' + id + '/resume').then(function() {
            ReefUtils.toast('Client ' + id + ' resumed', 'success');
            refresh();
            if (currentDetailId === id) refreshDetailInfo(id);
        }).catch(function(e) { ReefUtils.toast('Resume failed: ' + e.message, 'error'); });
    }

    function restart(id) {
        if (!confirm('Restart client ' + id + '? This may interrupt running tasks.')) return;
        ReefUtils.apiPost('/api/v2/client/' + id + '/restart').then(function() {
            ReefUtils.toast('Client ' + id + ' restarting...', 'info');
            refresh();
        }).catch(function(e) { ReefUtils.toast('Restart failed: ' + e.message, 'error'); });
    }

    // ---- View toggle ----
    function setView(mode) {
        viewMode = mode;
        // Update button styles
        var cardBtn = document.getElementById('view-card');
        var tableBtn = document.getElementById('view-table');
        if (cardBtn) {
            cardBtn.style.background = mode === 'card' ? 'var(--accent)' : '';
            cardBtn.style.color = mode === 'card' ? 'white' : '';
            cardBtn.style.borderColor = mode === 'card' ? 'var(--accent)' : '';
        }
        if (tableBtn) {
            tableBtn.style.background = mode === 'table' ? 'var(--accent)' : '';
            tableBtn.style.color = mode === 'table' ? 'white' : '';
            tableBtn.style.borderColor = mode === 'table' ? 'var(--accent)' : '';
        }
        renderClients();
    }

    // ---- Utils ----
    function debounce(fn, delay) {
        var timer;
        return function() {
            var ctx = this, args = arguments;
            clearTimeout(timer);
            timer = setTimeout(function() { fn.apply(ctx, args); }, delay);
        };
    }

    function cleanupDetail() {
        closeSessionStream();
        if (detailRefreshTimer) {
            clearInterval(detailRefreshTimer);
            detailRefreshTimer = null;
        }
        currentDetailId = null;
    }

    // ---- SSE handler for client_update ----
    function onClientUpdate(data) {
        // Auto-refresh the client list when updates arrive
        refresh();
    }

    return {
        render: render,
        renderDetail: renderDetail,
        refresh: refresh,
        setView: setView,
        pause: pause,
        resume: resume,
        restart: restart,
        onClientUpdate: onClientUpdate,
        cleanup: cleanupDetail
    };
})();
