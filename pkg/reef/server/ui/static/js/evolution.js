// evolution.js — Evolution Dashboard
'use strict';

var ReefEvolution = (function() {
    function render(container) {
        container.innerHTML = '' +
            '<div style="display:grid;grid-template-columns:1fr 1fr;gap:16px;">' +
                '<div class="card">' +
                    '<div class="card-header"><h2>Gene Library</h2>' +
                        '<select id="evo-filter-status" class="filter-select"><option value="">All</option><option value="draft">Draft</option><option value="submitted">Submitted</option><option value="approved">Approved</option><option value="rejected">Rejected</option></select>' +
                    '</div>' +
                    '<div class="table-container"><table class="data-table"><thead><tr><th>ID</th><th>Role</th><th>Signal</th><th>Status</th><th>Actions</th></tr></thead><tbody id="genes-body"></tbody></table></div>' +
                '</div>' +
                '<div>' +
                    '<div class="card"><h2>Evolution Strategy</h2>' +
                        '<div style="margin-top:12px;">' +
                            '<select id="evo-strategy" class="filter-select" style="min-width:160px;">' +
                                '<option value="balanced">Balanced (50/30/20)</option>' +
                                '<option value="innovate">Innovate (80/15/5)</option>' +
                                '<option value="harden">Harden (20/40/40)</option>' +
                                '<option value="repair-only">Repair Only (0/20/80)</option>' +
                            '</select>' +
                            '<button class="btn btn-primary btn-sm" style="margin-left:8px;" id="evo-apply-strategy">Apply</button>' +
                        '</div>' +
                    '</div>' +
                    '<div class="card"><h2>Evolution Timeline</h2>' +
                        '<div id="evo-timeline" class="evo-timeline" style="margin-top:12px;">' +
                            '<div style="color:var(--text-muted);font-size:12px;">No evolution events yet</div>' +
                        '</div>' +
                    '</div>' +
                    '<div class="card"><h2>Capsule Store</h2>' +
                        '<div id="capsule-store" style="margin-top:12px;">' +
                            '<div style="color:var(--text-muted);font-size:12px;">Loading...</div>' +
                        '</div>' +
                    '</div>' +
                '</div>' +
            '</div>';

        document.getElementById('evo-filter-status').addEventListener('change', loadGenes);
        document.getElementById('evo-apply-strategy').addEventListener('click', applyStrategy);
        loadGenes();
        loadStrategy();
        loadCapsules();
    }

    function refresh() { loadGenes(); loadStrategy(); }

    function loadGenes() {
        ReefUtils.apiGet('/api/v2/evolution/genes').then(function(data) {
            var tbody = document.getElementById('genes-body');
            if (!tbody) return;
            var genes = data.genes || data || [];
            var filter = (document.getElementById('evo-filter-status') || {}).value || '';
            tbody.innerHTML = '';
            if (genes.length === 0) {
                tbody.innerHTML = '<tr><td colspan="5" style="text-align:center;color:var(--text-muted);padding:16px;">No genes</td></tr>';
                return;
            }
            genes.filter(function(g) { return !filter || g.status === filter; }).forEach(function(g) {
                var tr = document.createElement('tr');
                var actions = '';
                if (g.status === 'submitted') {
                    actions = '<button class="btn btn-success btn-sm" onclick="ReefEvolution.approve(\'' + g.id + '\')">✓</button> ' +
                              '<button class="btn btn-danger btn-sm" onclick="ReefEvolution.reject(\'' + g.id + '\')">✗</button>';
                }
                var signalPct = Math.round((g.control_signal || 0) * 100);
                tr.innerHTML =
                    '<td class="mono">' + ReefUtils.escapeHtml(g.id) + '</td>' +
                    '<td>' + ReefUtils.escapeHtml(g.role) + '</td>' +
                    '<td><div class="progress-bar" style="width:80px;display:inline-block;vertical-align:middle;"><div class="progress-bar-fill" style="width:' + signalPct + '%"></div></div> ' + signalPct + '%</td>' +
                    '<td>' + ReefUtils.statusBadge(g.status) + '</td>' +
                    '<td>' + actions + '</td>';
                tbody.appendChild(tr);
            });
        }).catch(function() {});
    }

    function loadStrategy() {
        ReefUtils.apiGet('/api/v2/evolution/strategy').then(function(data) {
            var sel = document.getElementById('evo-strategy');
            if (sel && data.strategy) sel.value = data.strategy;
        }).catch(function() {});
    }

    function applyStrategy() {
        var sel = document.getElementById('evo-strategy');
        if (!sel) return;
        ReefUtils.apiPut('/api/v2/evolution/strategy', { strategy: sel.value })
            .then(function() { ReefUtils.toast('Strategy updated: ' + sel.value, 'success'); })
            .catch(function(e) { ReefUtils.toast('Failed: ' + e.message, 'error'); });
    }

    function loadCapsules() {
        ReefUtils.apiGet('/api/v2/evolution/capsules').then(function(data) {
            var el = document.getElementById('capsule-store');
            if (!el) return;
            var capsules = data.capsules || data || [];
            if (capsules.length === 0) {
                el.innerHTML = '<div style="color:var(--text-muted);font-size:12px;">No capsules available</div>';
                return;
            }
            el.innerHTML = capsules.map(function(c) {
                return '<div style="padding:8px;border:1px solid var(--border);border-radius:var(--radius);margin-bottom:8px;">' +
                    '<div style="font-weight:600;font-size:13px;">📦 ' + ReefUtils.escapeHtml(c.name) + '</div>' +
                    '<div style="font-size:11px;color:var(--text-muted);">' + ReefUtils.escapeHtml(c.role) + ' · ' + (c.skill_count||0) + ' skills</div>' +
                '</div>';
            }).join('');
        }).catch(function() {});
    }

    function approve(id) {
        ReefUtils.apiPost('/api/v2/evolution/genes/' + id + '/approve').then(function() {
            ReefUtils.toast('Gene approved', 'success'); loadGenes();
        }).catch(function(e) { ReefUtils.toast(e.message, 'error'); });
    }

    function reject(id) {
        var reason = prompt('Rejection reason (optional):');
        ReefUtils.apiPost('/api/v2/evolution/genes/' + id + '/reject', { reason: reason }).then(function() {
            ReefUtils.toast('Gene rejected', 'info'); loadGenes();
        }).catch(function(e) { ReefUtils.toast(e.message, 'error'); });
    }

    return { render: render, refresh: refresh, approve: approve, reject: reject };
})();
