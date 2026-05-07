// evolution.js — Evolution Dashboard
'use strict';

var ReefEvolution = (function() {
    var currentGenes = [];

    function render(container) {
        container.innerHTML = '' +
            '<div style="display:grid;grid-template-columns:1fr 1fr;gap:16px;">' +
                // Left: Gene Library + Submit Form
                '<div>' +
                    '<div class="card">' +
                        '<div class="card-header"><h2>Gene Library</h2>' +
                            '<select id="evo-filter-status" class="filter-select"><option value="">All</option><option value="draft">Draft</option><option value="submitted">Submitted</option><option value="approved">Approved</option><option value="rejected">Rejected</option></select>' +
                        '</div>' +
                        '<div class="table-container"><table class="data-table"><thead><tr><th>ID</th><th>Role</th><th>Signal</th><th>Status</th><th>Actions</th></tr></thead><tbody id="genes-body"></tbody></table></div>' +
                    '</div>' +

                    // Gene Signal Chart
                    '<div class="card" style="margin-top:16px;">' +
                        '<h2>Gene Signal Strengths</h2>' +
                        '<div id="gene-signal-chart" style="margin-top:12px;display:flex;flex-direction:column;gap:8px;">' +
                            '<div style="color:var(--text-muted);font-size:12px;">No genes loaded</div>' +
                        '</div>' +
                    '</div>' +

                    // Submit New Gene Form
                    '<div class="card" style="margin-top:16px;">' +
                        '<h2>Submit New Gene</h2>' +
                        '<form id="gene-submit-form" style="margin-top:12px;">' +
                            '<div class="form-row">' +
                                '<div class="form-group"><label>Role</label><input type="text" id="new-gene-role" required placeholder="e.g. coder, reviewer"></div>' +
                                '<div class="form-group"><label>Control Signal (0.0-1.0)</label><input type="number" id="new-gene-signal" value="0.5" min="0" max="1" step="0.01"></div>' +
                            '</div>' +
                            '<div class="form-group"><label>Description</label><textarea id="new-gene-desc" rows="2" placeholder="What this gene controls..."></textarea></div>' +
                            '<button type="submit" class="btn btn-primary btn-sm">Submit Gene</button>' +
                        '</form>' +
                    '</div>' +
                '</div>' +

                // Right: Strategy + Timeline + Capsules
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
                            '<div style="color:var(--text-muted);font-size:12px;">Loading events...</div>' +
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
        document.getElementById('gene-submit-form').addEventListener('submit', submitGene);
        loadGenes();
        loadStrategy();
        loadCapsules();
        loadTimeline();
    }

    function refresh() {
        loadGenes();
        loadStrategy();
        loadTimeline();
        loadCapsules();
    }

    // ---- Gene Library ----

    function loadGenes() {
        ReefUtils.apiGet('/api/v2/evolution/genes').then(function(data) {
            var tbody = document.getElementById('genes-body');
            if (!tbody) return;
            var genes = data.genes || data || [];
            currentGenes = genes;
            var filter = (document.getElementById('evo-filter-status') || {}).value || '';
            tbody.innerHTML = '';
            if (genes.length === 0) {
                tbody.innerHTML = '<tr><td colspan="5" style="text-align:center;color:var(--text-muted);padding:16px;">No genes</td></tr>';
                renderSignalChart([]);
                return;
            }
            var filtered = genes.filter(function(g) { return !filter || g.status === filter; });
            filtered.forEach(function(g) {
                var tr = document.createElement('tr');
                tr.style.cursor = 'pointer';
                var actions = '';
                if (g.status === 'submitted') {
                    actions = '<button class="btn btn-success btn-sm" onclick="event.stopPropagation();ReefEvolution.approve(\'' + g.id + '\')">✓</button> ' +
                              '<button class="btn btn-danger btn-sm" onclick="event.stopPropagation();ReefEvolution.reject(\'' + g.id + '\')">✗</button>';
                }
                var signalPct = Math.round((g.control_signal || 0) * 100);
                tr.innerHTML =
                    '<td class="mono">' + ReefUtils.escapeHtml(g.id) + '</td>' +
                    '<td>' + ReefUtils.escapeHtml(g.role) + '</td>' +
                    '<td><div class="progress-bar" style="width:80px;display:inline-block;vertical-align:middle;"><div class="progress-bar-fill" style="width:' + signalPct + '%"></div></div> ' + signalPct + '%</td>' +
                    '<td>' + ReefUtils.statusBadge(g.status) + '</td>' +
                    '<td>' + actions + '</td>';

                tr.addEventListener('click', function(e) {
                    if (e.target.tagName === 'BUTTON') return;
                    showGeneDetail(g);
                });
                tbody.appendChild(tr);
            });
            renderSignalChart(currentGenes);
        }).catch(function() {});
    }

    // ---- Gene Signal Chart (CSS bars) ----

    function renderSignalChart(genes) {
        var chart = document.getElementById('gene-signal-chart');
        if (!chart) return;
        if (!genes || genes.length === 0) {
            chart.innerHTML = '<div style="color:var(--text-muted);font-size:12px;">No genes loaded</div>';
            return;
        }

        // Show top 20 genes by signal strength
        var sorted = genes.slice().sort(function(a, b) { return (b.control_signal || 0) - (a.control_signal || 0); }).slice(0, 20);
        var maxSignal = sorted.length > 0 ? Math.max(sorted[0].control_signal || 0, 0.01) : 1;

        chart.innerHTML = sorted.map(function(g) {
            var pct = Math.round((g.control_signal || 0) * 100);
            var barW = Math.max(pct, 2);
            // Color based on signal strength
            var barColor = pct > 70 ? 'var(--success)' : (pct > 40 ? 'var(--info)' : (pct > 20 ? 'var(--warning)' : 'var(--text-muted)'));
            return '<div style="display:flex;align-items:center;gap:8px;font-size:12px;">' +
                '<div style="width:80px;text-align:right;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:var(--text-secondary);flex-shrink:0;" title="' + ReefUtils.escapeHtml(g.role) + '">' + ReefUtils.escapeHtml(ReefUtils.truncate(g.role, 10)) + '</div>' +
                '<div style="flex:1;height:14px;background:var(--bg-tertiary);border-radius:4px;overflow:hidden;">' +
                    '<div style="height:100%;width:' + barW + '%;background:' + barColor + ';border-radius:4px;transition:width 0.3s;min-width:2px;display:flex;align-items:center;justify-content:flex-end;padding-right:4px;font-size:10px;color:white;">' + (pct >= 15 ? pct + '%' : '') + '</div>' +
                '</div>' +
                '<div style="width:70px;font-size:10px;color:var(--text-muted);flex-shrink:0;">' + ReefUtils.statusBadge(g.status) + '</div>' +
            '</div>';
        }).join('');

        if (genes.length > 20) {
            chart.innerHTML += '<div style="font-size:11px;color:var(--text-muted);text-align:center;margin-top:4px;">Showing top 20 of ' + genes.length + ' genes</div>';
        }
    }

    // ---- Gene Detail Modal ----

    function showGeneDetail(gene) {
        var overlay = document.createElement('div');
        overlay.className = 'modal-overlay';
        overlay.id = 'gene-detail-overlay';
        var signalPct = Math.round((gene.control_signal || 0) * 100);
        var tsFields = [gene.created, gene.created_at, gene.submitted_at, gene.updated_at].filter(Boolean);
        var createdStr = tsFields.length > 0 ? ReefUtils.formatDateTime(tsFields[0]) : '--';

        overlay.innerHTML = '<div class="modal" style="max-width:500px;">' +
            '<div class="modal-header">' +
                '<h3>Gene: ' + ReefUtils.escapeHtml(gene.id) + '</h3>' +
                '<button class="modal-close" id="gene-detail-close">&times;</button>' +
            '</div>' +
            '<div style="display:grid;grid-template-columns:1fr 1fr;gap:12px;font-size:13px;margin-bottom:16px;">' +
                '<div><span style="color:var(--text-muted);font-size:11px;">Role</span><div style="font-weight:500;">' + ReefUtils.escapeHtml(gene.role) + '</div></div>' +
                '<div><span style="color:var(--text-muted);font-size:11px;">Status</span><div>' + ReefUtils.statusBadge(gene.status) + '</div></div>' +
                '<div><span style="color:var(--text-muted);font-size:11px;">Control Signal</span><div>' +
                    '<div class="progress-bar" style="width:100px;display:inline-block;vertical-align:middle;margin-right:8px;"><div class="progress-bar-fill" style="width:' + signalPct + '%"></div></div>' + signalPct + '%' +
                '</div></div>' +
                '<div><span style="color:var(--text-muted);font-size:11px;">Created</span><div>' + createdStr + '</div></div>' +
            '</div>' +
            (gene.description ? '<div style="margin-bottom:16px;"><span style="color:var(--text-muted);font-size:11px;">Description</span><div style="background:var(--bg-primary);padding:10px;border-radius:var(--radius);font-size:12px;margin-top:4px;line-height:1.5;">' + ReefUtils.escapeHtml(gene.description) + '</div></div>' : '') +
            (gene.reason ? '<div style="margin-bottom:16px;"><span style="color:var(--text-muted);font-size:11px;">Reason</span><div style="background:var(--bg-primary);padding:10px;border-radius:var(--radius);font-size:12px;margin-top:4px;">' + ReefUtils.escapeHtml(gene.reason) + '</div></div>' : '') +
            (gene.rejection_reason ? '<div style="margin-bottom:16px;"><span style="color:var(--error);font-size:11px;">Rejection Reason</span><div style="background:var(--error-light);padding:10px;border-radius:var(--radius);font-size:12px;margin-top:4px;color:var(--error);">' + ReefUtils.escapeHtml(gene.rejection_reason) + '</div></div>' : '') +
            // Extra metadata
            '<div style="display:grid;grid-template-columns:1fr 1fr;gap:8px;font-size:12px;color:var(--text-muted);padding-top:12px;border-top:1px solid var(--border);">' +
                '<div>Gene ID: <span class="mono">' + ReefUtils.escapeHtml(gene.id || '--') + '</span></div>' +
                (gene.version !== undefined ? '<div>Version: ' + gene.version + '</div>' : '') +
                (gene.generation !== undefined ? '<div>Generation: ' + gene.generation + '</div>' : '') +
                (gene.fitness !== undefined ? '<div>Fitness: ' + (typeof gene.fitness === 'number' ? gene.fitness.toFixed(3) : gene.fitness) + '</div>' : '') +
            '</div>' +
        '</div>';

        document.body.appendChild(overlay);
        overlay.addEventListener('click', function(e) {
            if (e.target === overlay) closeGeneDetail();
        });
        document.getElementById('gene-detail-close').addEventListener('click', closeGeneDetail);
    }

    function closeGeneDetail() {
        var overlay = document.getElementById('gene-detail-overlay');
        if (overlay) overlay.remove();
    }

    // ---- Submit New Gene ----

    function submitGene(e) {
        e.preventDefault();
        var role = document.getElementById('new-gene-role').value.trim();
        var signal = parseFloat(document.getElementById('new-gene-signal').value) || 0.5;
        var desc = document.getElementById('new-gene-desc').value.trim();

        if (!role) {
            ReefUtils.toast('Role is required', 'error');
            return;
        }

        ReefUtils.apiPost('/api/v2/evolution/genes', {
            role: role,
            control_signal: Math.max(0, Math.min(1, signal)),
            description: desc
        }).then(function(data) {
            ReefUtils.toast('Gene submitted: ' + (data.gene_id || data.id || ''), 'success');
            document.getElementById('new-gene-role').value = '';
            document.getElementById('new-gene-signal').value = '0.5';
            document.getElementById('new-gene-desc').value = '';
            loadGenes();
        }).catch(function(err) {
            ReefUtils.toast('Submit failed: ' + err.message, 'error');
        });
    }

    // ---- Strategy ----

    function loadStrategy() {
        ReefUtils.apiGet('/api/v2/evolution/strategy').then(function(data) {
            var sel = document.getElementById('evo-strategy');
            if (sel && data.strategy) sel.value = data.strategy;
        }).catch(function() {});
    }

    function applyStrategy() {
        var sel = document.getElementById('evo-strategy');
        if (!sel) return;
        var newStrategy = sel.value;
        var labelMap = {
            'balanced': 'Balanced (50/30/20)',
            'innovate': 'Innovate (80/15/5)',
            'harden': 'Harden (20/40/40)',
            'repair-only': 'Repair Only (0/20/80)'
        };

        // Show confirmation dialog
        var confirmed = confirm(
            'Change evolution strategy?\n\n' +
            'New strategy: ' + (labelMap[newStrategy] || newStrategy) + '\n\n' +
            'This will change how genes are selected for the next generation. Existing approved genes will not be affected.'
        );

        if (!confirmed) {
            // Revert dropdown
            loadStrategy();
            return;
        }

        ReefUtils.apiPut('/api/v2/evolution/strategy', { strategy: newStrategy })
            .then(function() { ReefUtils.toast('Strategy updated: ' + newStrategy, 'success'); })
            .catch(function(e) { ReefUtils.toast('Failed: ' + e.message, 'error'); });
    }

    // ---- Evolution Timeline ----

    function loadTimeline() {
        ReefUtils.apiGet('/api/v2/activity?limit=30&type=evolution').then(function(data) {
            var el = document.getElementById('evo-timeline');
            if (!el) return;
            var events = data.events || data.items || data || [];
            if (!Array.isArray(events) || events.length === 0) {
                el.innerHTML = '<div style="color:var(--text-muted);font-size:12px;">No evolution events yet</div>';
                return;
            }

            var icons = {
                'gene_approved': '✅',
                'gene_submitted': '📝',
                'gene_rejected': '❌',
                'gene_created': '🧬',
                'gene_mutated': '🔄',
                'strategy_changed': '⚙️',
                'generation_completed': '🏁',
                'capsule_installed': '📦',
                'capsule_created': '📦',
                'evolution_cycle': '🔄',
                'default': '📌'
            };

            el.innerHTML = events.map(function(ev) {
                var evt = ev.type || ev.event || ev.action || '';
                var icon = icons[evt] || icons['default'];
                var msg = ev.message || ev.description || ev.summary || evt;
                var time = ReefUtils.formatDateTime(ev.timestamp || ev.created_at || ev.time);
                return '<div class="evo-timeline-item">' +
                    '<div class="evo-timeline-time">' + time + '</div>' +
                    '<div class="evo-timeline-content">' +
                        icon + ' <span class="badge badge-' + (evt.toLowerCase().indexOf('reject') >= 0 ? 'rejected' : (evt.toLowerCase().indexOf('approv') >= 0 ? 'approved' : '')) + '" style="margin-right:4px;">' + ReefUtils.escapeHtml(evt) + '</span> ' +
                        ReefUtils.escapeHtml(ReefUtils.truncate(msg, 80)) +
                    '</div>' +
                '</div>';
            }).join('');

            if (events.length >= 30) {
                el.innerHTML += '<div style="font-size:11px;color:var(--text-muted);text-align:center;margin-top:8px;">Showing last 30 events</div>';
            }
        }).catch(function() {
            var el = document.getElementById('evo-timeline');
            if (el) el.innerHTML = '<div style="color:var(--text-muted);font-size:12px;">No evolution events yet</div>';
        });
    }

    // ---- Capsule Store ----

    function loadCapsules() {
        ReefUtils.apiGet('/api/v2/evolution/capsules').then(function(data) {
            var el = document.getElementById('capsule-store');
            if (!el) return;
            var capsules = data.capsules || data || [];
            if (!Array.isArray(capsules) || capsules.length === 0) {
                el.innerHTML = '<div style="color:var(--text-muted);font-size:12px;">No capsules available</div>';
                return;
            }
            el.innerHTML = capsules.map(function(c) {
                return '<div style="padding:10px 12px;border:1px solid var(--border);border-radius:var(--radius);margin-bottom:8px;display:flex;justify-content:space-between;align-items:center;">' +
                    '<div>' +
                        '<div style="font-weight:600;font-size:13px;">📦 ' + ReefUtils.escapeHtml(c.name) + '</div>' +
                        '<div style="font-size:11px;color:var(--text-muted);">' + ReefUtils.escapeHtml(c.role) + ' · ' + (c.skill_count || 0) + ' skills' +
                            (c.version ? ' · v' + c.version : '') +
                        '</div>' +
                    '</div>' +
                    '<button class="btn btn-primary btn-sm" onclick="ReefEvolution.installCapsule(\'' + (c.id || '') + '\', \'' + ReefUtils.escapeHtml(c.name || '') + '\')">Install</button>' +
                '</div>';
            }).join('');
        }).catch(function() {});
    }

    function installCapsule(capsuleId, capsuleName) {
        if (!capsuleId) {
            ReefUtils.toast('Invalid capsule ID', 'error');
            return;
        }
        if (!confirm('Install capsule "' + (capsuleName || capsuleId) + '" to the gene library?\n\nThis will add the capsule\'s genes and skills to the current generation.')) {
            return;
        }
        ReefUtils.apiPost('/api/v2/evolution/capsules/' + capsuleId + '/install').then(function() {
            ReefUtils.toast('Capsule installed: ' + (capsuleName || capsuleId), 'success');
            loadGenes();
            loadTimeline();
        }).catch(function(err) {
            ReefUtils.toast('Install failed: ' + err.message, 'error');
        });
    }

    // ---- Gene Actions ----

    function approve(id) {
        ReefUtils.apiPost('/api/v2/evolution/genes/' + id + '/approve').then(function() {
            ReefUtils.toast('Gene approved', 'success');
            loadGenes();
            loadTimeline();
        }).catch(function(e) { ReefUtils.toast(e.message, 'error'); });
    }

    function reject(id) {
        var reason = prompt('Rejection reason (optional):');
        ReefUtils.apiPost('/api/v2/evolution/genes/' + id + '/reject', { reason: reason }).then(function() {
            ReefUtils.toast('Gene rejected', 'info');
            loadGenes();
            loadTimeline();
        }).catch(function(e) { ReefUtils.toast(e.message, 'error'); });
    }

    return {
        render: render, refresh: refresh,
        approve: approve, reject: reject,
        showGeneDetail: showGeneDetail, closeGeneDetail: closeGeneDetail,
        installCapsule: installCapsule
    };
})();
