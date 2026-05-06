// clients.js — Clients page (card + table view)
'use strict';

var ReefClients = (function() {
    var viewMode = 'card'; // 'card' or 'table'
    var clientsData = [];

    function render(container) {
        container.innerHTML = '' +
            '<div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:16px;">' +
                '<div class="filters">' +
                    '<select id="client-filter-state" class="filter-select">' +
                        '<option value="">All States</option><option value="connected">Online</option><option value="disconnected">Offline</option><option value="stale">Stale</option>' +
                    '</select>' +
                '</div>' +
                '<div style="display:flex;gap:8px;">' +
                    '<button class="btn btn-secondary btn-sm" id="view-card" onclick="ReefClients.setView(\'card\')">📇 Cards</button>' +
                    '<button class="btn btn-secondary btn-sm" id="view-table" onclick="ReefClients.setView(\'table\')">📊 Table</button>' +
                '</div>' +
            '</div>' +
            '<div id="clients-container"></div>';

        document.getElementById('client-filter-state').addEventListener('change', refresh);
        refresh();
    }

    function refresh() {
        ReefUtils.apiGet('/api/v2/clients').then(function(data) {
            clientsData = data || [];
            renderClients();
        }).catch(function() {});
    }

    function renderClients() {
        var container = document.getElementById('clients-container');
        if (!container) return;
        var filter = (document.getElementById('client-filter-state') || {}).value || '';
        var filtered = clientsData.filter(function(c) {
            if (!filter) return true;
            return c.state === filter;
        });

        if (filtered.length === 0) {
            container.innerHTML = '<div class="empty-state"><div class="empty-icon">🤖</div><div class="empty-text">No clients connected</div></div>';
            return;
        }

        if (viewMode === 'card') renderCardView(container, filtered);
        else renderTableView(container, filtered);
    }

    function renderCardView(container, clients) {
        var html = '<div class="agent-grid">';
        clients.forEach(function(c) {
            var initials = (c.id || '?')[0].toUpperCase();
            html += '<div class="agent-card" onclick="ReefApp.go(\'/clients/detail?id=' + c.id + '\')">' +
                '<div class="agent-card-header">' +
                    '<div class="agent-avatar">' + initials + '<span class="status-dot ' + ReefUtils.stateClass(c.state) + '"></span></div>' +
                    '<div><div class="agent-card-name">' + ReefUtils.escapeHtml(c.id) + '</div>' +
                    '<div class="agent-card-role">' + ReefUtils.escapeHtml(c.role) + '</div></div>' +
                '</div>' +
                '<div style="margin-bottom:8px;">' + ReefUtils.skillBadges(c.skills) + '</div>' +
                '<div class="agent-card-stats"><span>Load: ' + c.load + '</span></div>' +
            '</div>';
        });
        html += '</div>';
        container.innerHTML = html;
    }

    function renderTableView(container, clients) {
        var html = '<div class="table-container"><table class="data-table"><thead><tr>' +
            '<th>ID</th><th>Role</th><th>Skills</th><th>State</th><th>Load</th><th>Heartbeat</th></tr></thead><tbody>';
        clients.forEach(function(c) {
            html += '<tr style="cursor:pointer;" onclick="ReefApp.go(\'/clients/detail?id=' + c.id + '\')">' +
                '<td class="mono">' + ReefUtils.escapeHtml(c.id) + '</td>' +
                '<td>' + ReefUtils.escapeHtml(c.role) + '</td>' +
                '<td>' + ReefUtils.skillBadges(c.skills) + '</td>' +
                '<td><span class="status-dot ' + ReefUtils.stateClass(c.state) + '"></span> ' + ReefUtils.escapeHtml(c.state) + '</td>' +
                '<td>' + c.load + '</td>' +
                '<td>' + ReefUtils.formatTime(c.last_heartbeat) + '</td></tr>';
        });
        html += '</tbody></table></div>';
        container.innerHTML = html;
    }

    function renderDetail(container, clientId) {
        if (!clientId) { ReefApp.go('/clients'); return; }
        container.innerHTML = '<div style="color:var(--text-muted);">Loading client ' + ReefUtils.escapeHtml(clientId) + '...</div>';

        ReefUtils.apiGet('/api/v2/client/' + clientId).then(function(client) {
            container.innerHTML = '' +
                '<div style="margin-bottom:16px;">' +
                    '<button class="btn btn-secondary btn-sm" onclick="ReefApp.go(\'/clients\')">← Back</button>' +
                '</div>' +
                '<div class="card">' +
                    '<div style="display:flex;align-items:center;gap:16px;margin-bottom:16px;">' +
                        '<div class="agent-avatar" style="width:48px;height:48px;font-size:18px;">' +
                            (client.id||'?')[0].toUpperCase() +
                            '<span class="status-dot ' + ReefUtils.stateClass(client.state) + '" style="width:12px;height:12px;"></span>' +
                        '</div>' +
                        '<div>' +
                            '<h2>' + ReefUtils.escapeHtml(client.id) + '</h2>' +
                            '<div style="color:var(--text-secondary);font-size:13px;">Role: ' + ReefUtils.escapeHtml(client.role) + ' · Skills: ' + ReefUtils.skillBadges(client.skills) + '</div>' +
                        '</div>' +
                    '</div>' +
                    '<div style="display:flex;gap:12px;margin-bottom:16px;">' +
                        '<button class="btn btn-secondary btn-sm" onclick="ReefClients.pause(\'' + client.id + '\')">⏸ Pause</button>' +
                        '<button class="btn btn-secondary btn-sm" onclick="ReefClients.resume(\'' + client.id + '\')">▶ Resume</button>' +
                        '<button class="btn btn-danger btn-sm" onclick="ReefClients.restart(\'' + client.id + '\')">🔄 Restart</button>' +
                    '</div>' +
                '</div>' +
                '<div class="card">' +
                    '<h3>Live Execution Stream</h3>' +
                    '<div id="client-session-stream" style="margin-top:12px;max-height:400px;overflow-y:auto;font-family:monospace;font-size:12px;color:var(--text-secondary);">' +
                        '<div style="color:var(--text-muted);">Waiting for events...</div>' +
                    '</div>' +
                '</div>';
        }).catch(function(err) {
            container.innerHTML = '<div class="empty-state"><div class="empty-icon">❌</div><div class="empty-text">Client not found: ' + ReefUtils.escapeHtml(err.message) + '</div></div>';
        });
    }

    function pause(id) { ReefUtils.apiPost('/api/v2/client/' + id + '/pause').then(function() { ReefUtils.toast('Paused', 'info'); refresh(); }).catch(function(e) { ReefUtils.toast(e.message, 'error'); }); }
    function resume(id) { ReefUtils.apiPost('/api/v2/client/' + id + '/resume').then(function() { ReefUtils.toast('Resumed', 'success'); refresh(); }).catch(function(e) { ReefUtils.toast(e.message, 'error'); }); }
    function restart(id) { if (!confirm('Restart ' + id + '?')) return; ReefUtils.apiPost('/api/v2/client/' + id + '/restart').then(function() { ReefUtils.toast('Restarting...', 'info'); refresh(); }).catch(function(e) { ReefUtils.toast(e.message, 'error'); }); }

    function setView(mode) { viewMode = mode; renderClients(); }

    return { render: render, renderDetail: renderDetail, refresh: refresh, setView: setView, pause: pause, resume: resume, restart: restart };
})();
