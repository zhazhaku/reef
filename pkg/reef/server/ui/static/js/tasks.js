// tasks.js — Tasks list, submit, decompose
'use strict';

var ReefTasks = (function() {
    var offset = 0;
    var limit = 50;
    var selectedTasks = {};

    function render(container) {
        selectedTasks = {};
        container.innerHTML = '' +
            '<div class="card">' +
                '<div class="card-header">' +
                    '<h2>Tasks</h2>' +
                    '<div class="filters">' +
                        '<select id="task-filter-status" class="filter-select"><option value="">All Statuses</option>' +
                            '<option value="Queued">Queued</option><option value="Running">Running</option><option value="Completed">Completed</option>' +
                            '<option value="Failed">Failed</option><option value="Cancelled">Cancelled</option><option value="Escalated">Escalated</option></select>' +
                        '<input type="text" id="task-filter-role" class="filter-input" placeholder="Filter by role...">' +
                        '<button class="btn btn-primary btn-sm" onclick="ReefApp.go(\'/tasks/new\')">+ New Task</button>' +
                    '</div>' +
                '</div>' +
                // Status Color Legend
                '<div id="task-legend" style="display:flex;gap:12px;flex-wrap:wrap;padding:8px 0;font-size:11px;color:var(--text-secondary);border-bottom:1px solid var(--border);margin-bottom:8px;">' +
                    '<span><span class="badge badge-queued">Queued</span> Waiting for assignment</span>' +
                    '<span><span class="badge badge-running">Running</span> Agent is processing</span>' +
                    '<span><span class="badge badge-completed">Completed</span> Finished successfully</span>' +
                    '<span><span class="badge badge-failed">Failed</span> Error or timeout</span>' +
                    '<span><span class="badge badge-escalated">Escalated</span> Needs attention</span>' +
                    '<span><span class="badge badge-cancelled">Cancelled</span> Terminated</span>' +
                '</div>' +
                // Bulk Actions Bar
                '<div id="task-bulk-bar" style="display:none;padding:6px 0;align-items:center;gap:10px;">' +
                    '<span id="task-bulk-count" style="font-size:12px;color:var(--text-secondary);"></span>' +
                    '<button class="btn btn-danger btn-sm" id="task-bulk-cancel">Cancel Selected</button>' +
                    '<button class="btn btn-secondary btn-sm" id="task-bulk-clear">Clear Selection</button>' +
                '</div>' +
                '<div class="table-container"><table class="data-table"><thead><tr>' +
                    '<th style="width:32px;"><input type="checkbox" id="task-select-all" title="Select All Queued"></th>' +
                    '<th>ID</th><th>Status</th><th>Role</th><th>Instruction</th><th>Assigned</th><th>Created</th><th>Actions</th>' +
                '</tr></thead><tbody id="tasks-body"></tbody></table></div>' +
                '<div class="pagination" id="tasks-pagination"></div>' +
            '</div>';

        document.getElementById('task-filter-status').addEventListener('change', function() { offset = 0; loadTasks(); });
        document.getElementById('task-select-all').addEventListener('change', function() { toggleSelectAll(this.checked); });
        document.getElementById('task-bulk-cancel').addEventListener('click', bulkCancel);
        document.getElementById('task-bulk-clear').addEventListener('click', clearSelection);
        loadTasks();
    }

    function refresh() { loadTasks(); }

    function loadTasks() {
        var status = (document.getElementById('task-filter-status') || {}).value || '';
        var role = (document.getElementById('task-filter-role') || {}).value || '';
        var url = '/api/v2/tasks?limit=' + limit + '&offset=' + offset;
        if (status) url += '&status=' + encodeURIComponent(status);
        if (role) url += '&role=' + encodeURIComponent(role);

        ReefUtils.apiGet(url).then(function(data) {
            var tbody = document.getElementById('tasks-body');
            if (!tbody) return;
            if (!data.tasks || data.tasks.length === 0) {
                tbody.innerHTML = '<tr><td colspan="8" style="text-align:center;padding:40px 24px;">' +
                    '<div style="font-size:40px;margin-bottom:8px;">📋</div>' +
                    '<div style="color:var(--text-muted);font-size:14px;margin-bottom:4px;">No tasks found</div>' +
                    '<div style="color:var(--text-muted);font-size:12px;">Try adjusting filters or submit a new task</div>' +
                '</td></tr>';
                return;
            }
            tbody.innerHTML = '';
            data.tasks.forEach(function(t) {
                var tr = document.createElement('tr');
                tr.style.cursor = 'pointer';

                var isQueued = t.status === 'Queued';
                var isRunning = t.status === 'Running';
                var canCancel = isQueued || isRunning;
                var canPause = isRunning;
                var canResume = t.status === 'Paused';

                var actions = '';
                if (canCancel) {
                    actions = '<button class="btn btn-danger btn-sm" onclick="event.stopPropagation();ReefTasks.cancel(\'' + t.id + '\')">Cancel</button>';
                }
                if (canPause) {
                    actions += ' <button class="btn btn-secondary btn-sm" onclick="event.stopPropagation();ReefTasks.pause(\'' + t.id + '\')">⏸</button>';
                }
                if (canResume) {
                    actions = '<button class="btn btn-primary btn-sm" onclick="event.stopPropagation();ReefTasks.resume(\'' + t.id + '\')">▶</button>';
                }

                var cb = isQueued ? '<input type="checkbox" class="task-row-check" data-id="' + ReefUtils.escapeHtml(t.id) + '" onclick="event.stopPropagation();">' : '';

                tr.innerHTML =
                    '<td>' + cb + '</td>' +
                    '<td class="mono">' + ReefUtils.escapeHtml(t.id) + '</td>' +
                    '<td>' + ReefUtils.statusBadge(t.status) + '</td>' +
                    '<td>' + ReefUtils.escapeHtml(t.required_role) + '</td>' +
                    '<td>' + ReefUtils.escapeHtml(ReefUtils.truncate(t.instruction, 60)) + '</td>' +
                    '<td class="mono">' + ReefUtils.escapeHtml(t.assigned_client || '--') + '</td>' +
                    '<td>' + ReefUtils.formatTime(t.created_at) + '</td>' +
                    '<td>' + actions + '</td>';

                tr.addEventListener('click', function(e) {
                    // Don't trigger if clicking a button or checkbox
                    if (e.target.tagName === 'BUTTON' || e.target.tagName === 'INPUT' || e.target.tagName === 'SELECT') return;
                    ReefTasks.showDetail(t.id);
                });

                tbody.appendChild(tr);
            });

            // Re-attach checkbox listeners
            var checkboxes = tbody.querySelectorAll('.task-row-check');
            checkboxes.forEach(function(cb) {
                cb.addEventListener('change', function() {
                    var id = this.getAttribute('data-id');
                    if (this.checked) {
                        selectedTasks[id] = true;
                    } else {
                        delete selectedTasks[id];
                    }
                    updateBulkBar();
                });
            });

            renderPagination(data.total, data.limit, data.offset);
        }).catch(function() {});
    }

    function toggleSelectAll(checked) {
        var checkboxes = document.querySelectorAll('.task-row-check');
        checkboxes.forEach(function(cb) {
            cb.checked = checked;
            var id = cb.getAttribute('data-id');
            if (checked) {
                selectedTasks[id] = true;
            } else {
                delete selectedTasks[id];
            }
        });
        updateBulkBar();
    }

    function updateBulkBar() {
        var bar = document.getElementById('task-bulk-bar');
        var count = document.getElementById('task-bulk-count');
        var selAll = document.getElementById('task-select-all');
        if (!bar || !count) return;
        var keys = Object.keys(selectedTasks);
        if (keys.length > 0) {
            bar.style.display = 'flex';
            count.textContent = keys.length + ' task(s) selected';
            // Update select-all checkbox
            var totalCheckboxes = document.querySelectorAll('.task-row-check');
            if (selAll) selAll.checked = keys.length === totalCheckboxes.length && totalCheckboxes.length > 0;
        } else {
            bar.style.display = 'none';
            if (selAll) selAll.checked = false;
        }
    }

    function clearSelection() {
        selectedTasks = {};
        var checkboxes = document.querySelectorAll('.task-row-check');
        checkboxes.forEach(function(cb) { cb.checked = false; });
        var selAll = document.getElementById('task-select-all');
        if (selAll) selAll.checked = false;
        updateBulkBar();
    }

    function bulkCancel() {
        var keys = Object.keys(selectedTasks);
        if (keys.length === 0) return;
        if (!confirm('Cancel ' + keys.length + ' selected task(s)?')) return;

        var completed = 0;
        var errors = 0;
        keys.forEach(function(id) {
            ReefUtils.apiPost('/api/v2/tasks/' + id + '/cancel').then(function() {
                completed++;
                if (completed + errors === keys.length) finishBulk(completed, errors);
            }).catch(function() {
                errors++;
                if (completed + errors === keys.length) finishBulk(completed, errors);
            });
        });
    }

    function finishBulk(completed, errors) {
        ReefUtils.toast('Bulk cancel: ' + completed + ' succeeded, ' + errors + ' failed', errors > 0 ? 'error' : 'success');
        clearSelection();
        loadTasks();
    }

    // ---- Task Detail Modal ----

    function showDetail(taskId) {
        // Show loading modal
        var overlay = document.createElement('div');
        overlay.className = 'modal-overlay';
        overlay.id = 'task-detail-overlay';
        overlay.innerHTML = '<div class="modal"><div style="text-align:center;padding:24px;color:var(--text-muted);">Loading task ' + ReefUtils.escapeHtml(taskId) + '...</div></div>';
        document.body.appendChild(overlay);
        overlay.addEventListener('click', function(e) {
            if (e.target === overlay) closeDetail();
        });

        ReefUtils.apiGet('/api/v2/tasks/' + taskId).then(function(task) {
            renderDetailModal(overlay, task);
        }).catch(function(err) {
            overlay.querySelector('.modal').innerHTML =
                '<div class="modal-header"><h3>Error</h3><button class="modal-close" onclick="ReefTasks.closeDetail()">×</button></div>' +
                '<div style="color:var(--error);padding:16px;">Failed to load task: ' + err.message + '</div>';
        });
    }

    function closeDetail() {
        var overlay = document.getElementById('task-detail-overlay');
        if (overlay) overlay.remove();
    }

    function renderDetailModal(overlay, task) {
        var statusTimeline = buildStatusTimeline(task);
        var attemptRows = buildAttemptRows(task);
        var resultBlock = buildResultBlock(task);

        var canCancel = task.status === 'Queued' || task.status === 'Running';
        var canPause = task.status === 'Running';
        var canResume = task.status === 'Paused';

        var actionButtons = '';
        if (canCancel) actionButtons += '<button class="btn btn-danger btn-sm" id="detail-cancel-btn">Cancel Task</button> ';
        if (canPause) actionButtons += '<button class="btn btn-secondary btn-sm" id="detail-pause-btn">⏸ Pause</button> ';
        if (canResume) actionButtons += '<button class="btn btn-primary btn-sm" id="detail-resume-btn">▶ Resume</button>';

        overlay.querySelector('.modal').innerHTML = '' +
            '<div class="modal-header">' +
                '<h3>Task ' + ReefUtils.escapeHtml(task.id) + '</h3>' +
                '<button class="modal-close" onclick="ReefTasks.closeDetail()">&times;</button>' +
            '</div>' +
            '<div style="margin-bottom:16px;">' + ReefUtils.statusBadge(task.status) + '</div>' +

            // Instruction
            '<div style="margin-bottom:16px;">' +
                '<div style="font-weight:600;font-size:12px;color:var(--text-secondary);margin-bottom:4px;">Instruction</div>' +
                '<div style="background:var(--bg-primary);padding:12px;border-radius:var(--radius);font-size:13px;white-space:pre-wrap;line-height:1.5;">' + ReefUtils.escapeHtml(task.instruction || '--') + '</div>' +
            '</div>' +

            // Meta info grid
            '<div style="display:grid;grid-template-columns:1fr 1fr;gap:10px;margin-bottom:16px;font-size:12px;">' +
                '<div><span style="color:var(--text-muted);">Role:</span> ' + ReefUtils.escapeHtml(task.required_role || '--') + '</div>' +
                '<div><span style="color:var(--text-muted);">Assigned:</span> ' + ReefUtils.escapeHtml(task.assigned_client || '--') + '</div>' +
                '<div><span style="color:var(--text-muted);">Skills:</span> ' + (task.required_skills ? task.required_skills.join(', ') : '--') + '</div>' +
                '<div><span style="color:var(--text-muted);">Created:</span> ' + ReefUtils.formatDateTime(task.created_at) + '</div>' +
                '<div><span style="color:var(--text-muted);">Escalation Count:</span> ' + (task.escalation_count || 0) + '</div>' +
                '<div><span style="color:var(--text-muted);">Retries:</span> ' + (task.retry_count || 0) + '/' + (task.max_retries || 0) + '</div>' +
            '</div>' +

            // Status Timeline
            '<div style="margin-bottom:16px;">' +
                '<div style="font-weight:600;font-size:12px;color:var(--text-secondary);margin-bottom:8px;">Status Timeline</div>' +
                '<div style="display:flex;align-items:center;gap:0;font-size:11px;flex-wrap:wrap;">' + statusTimeline + '</div>' +
            '</div>' +

            // Attempt History
            '<div style="margin-bottom:16px;">' +
                '<div style="font-weight:600;font-size:12px;color:var(--text-secondary);margin-bottom:8px;">Attempt History</div>' +
                (attemptRows ?
                    '<table class="data-table" style="font-size:11px;"><thead><tr><th>#</th><th>Start</th><th>End</th><th>Status</th><th>Error</th></tr></thead><tbody>' + attemptRows + '</tbody></table>' :
                    '<div style="color:var(--text-muted);font-size:12px;">No attempts recorded</div>') +
            '</div>' +

            // Result / Error
            resultBlock +

            // Actions
            '<div style="margin-top:16px;padding-top:12px;border-top:1px solid var(--border);display:flex;gap:8px;">' +
                actionButtons +
            '</div>';

        // Bind action buttons
        var cancelBtn = document.getElementById('detail-cancel-btn');
        var pauseBtn = document.getElementById('detail-pause-btn');
        var resumeBtn = document.getElementById('detail-resume-btn');

        if (cancelBtn) cancelBtn.addEventListener('click', function() {
            if (confirm('Cancel task ' + task.id + '?')) {
                ReefTasks.cancel(task.id);
                closeDetail();
            }
        });
        if (pauseBtn) pauseBtn.addEventListener('click', function() {
            ReefTasks.pause(task.id);
            closeDetail();
        });
        if (resumeBtn) resumeBtn.addEventListener('click', function() {
            ReefTasks.resume(task.id);
            closeDetail();
        });
    }

    function buildStatusTimeline(task) {
        var phases = ['Created', 'Queued', 'Assigned', 'Running', 'Completed'];
        var statusMap = {
            'Created': !!task.created_at,
            'Queued': true,
            'Assigned': !!task.assigned_client,
            'Running': task.status === 'Running' || task.status === 'Completed' || task.status === 'Failed' || task.status === 'Cancelled',
            'Completed': task.status === 'Completed'
        };
        var currentIdx = phases.indexOf(task.status) >= 0 ? phases.indexOf(task.status) : (task.status === 'Failed' || task.status === 'Cancelled' || task.status === 'Escalated' ? phases.indexOf('Running') : 0);

        var html = '';
        phases.forEach(function(phase, i) {
            var isCompleted = statusMap[phase];
            var isCurrent = (i === currentIdx && task.status !== 'Completed') || (phase === 'Completed' && task.status === 'Completed');
            var isFailed = (i === phases.indexOf('Running') && (task.status === 'Failed' || task.status === 'Cancelled'));
            var color = isFailed ? 'var(--error)' : (isCompleted ? 'var(--success)' : (isCurrent ? 'var(--info)' : 'var(--text-muted)'));
            var bg = isFailed ? 'var(--error-light)' : (isCompleted ? 'var(--success-light)' : (isCurrent ? 'var(--info-light)' : 'var(--bg-tertiary)'));

            html += '<div style="display:flex;align-items:center;gap:0;">';
            html += '<div style="width:24px;height:24px;border-radius:50%;background:' + bg + ';border:2px solid ' + color + ';display:flex;align-items:center;justify-content:center;font-size:10px;">' + (isCompleted ? '✓' : (isCurrent ? '●' : (isFailed ? '✗' : '○'))) + '</div>';
            if (i < phases.length - 1) {
                html += '<div style="width:28px;height:2px;background:' + (isCompleted ? 'var(--success)' : 'var(--border)') + ';"></div>';
            }
            html += '</div>';
        });

        // Labels below
        html += '<div style="display:flex;gap:0;width:100%;margin-top:4px;">';
        phases.forEach(function(phase, i) {
            var extraSpace = i < phases.length - 1 ? 28 : 0;
            html += '<div style="width:' + (24 + extraSpace) + 'px;text-align:center;font-size:10px;color:var(--text-muted);">' + phase + '</div>';
        });
        html += '</div>';

        // For Failed/Cancelled, show that ending
        if (task.status === 'Failed' || task.status === 'Cancelled' || task.status === 'Escalated') {
            html += '<div style="margin-top:6px;">' +
                '<span class="badge badge-' + task.status.toLowerCase() + '">' + task.status + '</span>' +
            '</div>';
        }

        return html;
    }

    function buildAttemptRows(task) {
        var attempts = task.attempts || task.attempt_history || [];
        if (attempts.length === 0) return null;
        return attempts.map(function(a, i) {
            return '<tr>' +
                '<td>' + (i + 1) + '</td>' +
                '<td>' + ReefUtils.formatDateTime(a.start_time || a.started_at) + '</td>' +
                '<td>' + ReefUtils.formatDateTime(a.end_time || a.ended_at || a.completed_at) + '</td>' +
                '<td>' + ReefUtils.statusBadge(a.status || 'unknown') + '</td>' +
                '<td style="color:var(--error);max-width:200px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;" title="' + ReefUtils.escapeHtml(a.error || '') + '">' + ReefUtils.escapeHtml(ReefUtils.truncate(a.error || '--', 40)) + '</td>' +
            '</tr>';
        }).join('');
    }

    function buildResultBlock(task) {
        var html = '';
        if (task.result) {
            html += '<div style="margin-bottom:16px;">' +
                '<div style="font-weight:600;font-size:12px;color:var(--text-secondary);margin-bottom:4px;">Result</div>' +
                '<div style="background:var(--bg-primary);padding:12px;border-radius:var(--radius);font-size:12px;max-height:160px;overflow-y:auto;white-space:pre-wrap;">' +
                    ReefUtils.escapeHtml(typeof task.result === 'string' ? task.result : JSON.stringify(task.result, null, 2)) +
                '</div>' +
            '</div>';
        }
        if (task.error) {
            html += '<div style="margin-bottom:16px;">' +
                '<div style="font-weight:600;font-size:12px;color:var(--error);margin-bottom:4px;">Error</div>' +
                '<div style="background:var(--error-light);padding:12px;border-radius:var(--radius);font-size:12px;max-height:120px;overflow-y:auto;white-space:pre-wrap;color:var(--error);">' +
                    ReefUtils.escapeHtml(typeof task.error === 'string' ? task.error : JSON.stringify(task.error, null, 2)) +
                '</div>' +
            '</div>';
        }
        return html;
    }

    // ---- Task Actions ----

    function pause(taskId) {
        if (!confirm('Pause task ' + taskId + '?')) return;
        ReefUtils.apiPost('/api/v2/tasks/' + taskId + '/pause').then(function() {
            ReefUtils.toast('Task paused', 'info');
            loadTasks();
        }).catch(function(e) { ReefUtils.toast(e.message, 'error'); });
    }

    function resume(taskId) {
        ReefUtils.apiPost('/api/v2/tasks/' + taskId + '/resume').then(function() {
            ReefUtils.toast('Task resumed', 'success');
            loadTasks();
        }).catch(function(e) { ReefUtils.toast(e.message, 'error'); });
    }

    function cancel(taskId) {
        if (!confirm('Cancel task ' + taskId + '?')) return;
        ReefUtils.apiPost('/api/v2/tasks/' + taskId + '/cancel').then(function() {
            ReefUtils.toast('Task cancelled', 'info');
            loadTasks();
        }).catch(function(e) { ReefUtils.toast(e.message, 'error'); });
    }

    // ---- Pagination ----

    function renderPagination(total, lim, off) {
        var c = document.getElementById('tasks-pagination');
        if (!c) return;
        c.innerHTML = '';
        if (total <= lim) return;
        var totalPages = Math.ceil(total / lim);
        var cur = Math.floor(off / lim) + 1;
        var prev = document.createElement('button');
        prev.textContent = '← Prev'; prev.disabled = off === 0;
        prev.onclick = function() { offset = Math.max(0, off - lim); loadTasks(); };
        var info = document.createElement('span');
        info.className = 'page-info'; info.textContent = 'Page ' + cur + ' of ' + totalPages;
        var next = document.createElement('button');
        next.textContent = 'Next →'; next.disabled = off + lim >= total;
        next.onclick = function() { offset = off + lim; loadTasks(); };
        c.appendChild(prev); c.appendChild(info); c.appendChild(next);
    }

    // ---- Task Submit ----

    function renderSubmit(container) {
        container.innerHTML = '' +
            '<div style="margin-bottom:16px;"><button class="btn btn-secondary btn-sm" onclick="ReefApp.go(\'/tasks\')">← Back to Tasks</button></div>' +
            '<div class="card">' +
                '<h2>Submit New Task</h2>' +
                '<form id="task-submit-form" style="margin-top:16px;">' +
                    '<div class="form-group"><label>Instruction</label><textarea id="new-instruction" rows="4" required placeholder="Describe the task..."></textarea></div>' +
                    '<div class="form-row">' +
                        '<div class="form-group"><label>Required Role</label><input type="text" id="new-role" required placeholder="e.g. coder, tester"></div>' +
                        '<div class="form-group"><label>Skills (comma-separated)</label><input type="text" id="new-skills" placeholder="e.g. go, python"></div>' +
                    '</div>' +
                    '<div class="form-row">' +
                        '<div class="form-group"><label>Max Retries</label><input type="number" id="new-retries" value="3" min="0"></div>' +
                        '<div class="form-group"><label>Timeout (seconds)</label><input type="number" id="new-timeout" value="300" min="0"></div>' +
                    '</div>' +
                    '<button type="submit" class="btn btn-primary">Submit Task</button>' +
                    '<div id="submit-result" class="submit-result hidden"></div>' +
                '</form>' +
            '</div>';

        document.getElementById('task-submit-form').addEventListener('submit', function(e) {
            e.preventDefault();
            var instruction = document.getElementById('new-instruction').value.trim();
            var role = document.getElementById('new-role').value.trim();
            var skills = document.getElementById('new-skills').value.trim();
            if (!instruction || !role) return;
            ReefUtils.apiPost('/tasks', {
                instruction: instruction, required_role: role,
                required_skills: skills ? skills.split(',').map(function(s) { return s.trim(); }) : []
            }).then(function(data) {
                ReefUtils.toast('Task submitted: ' + (data.task_id || ''), 'success');
                ReefApp.go('/tasks');
            }).catch(function(err) {
                ReefUtils.toast('Submit failed: ' + err.message, 'error');
            });
        });
    }

    // ---- Task Decompose ----

    function renderDecompose(container, taskId) {
        if (!taskId) { ReefApp.go('/tasks'); return; }
        container.innerHTML = '<div style="color:var(--text-muted);">Loading decomposition for ' + ReefUtils.escapeHtml(taskId) + '...</div>';

        ReefUtils.apiGet('/api/v2/tasks/' + taskId + '/decompose').then(function(data) {
            container.innerHTML = '' +
                '<div style="margin-bottom:16px;"><button class="btn btn-secondary btn-sm" onclick="ReefApp.go(\'/tasks\')">← Back</button></div>' +
                '<div class="card"><h2>Task Decomposition: ' + ReefUtils.escapeHtml(taskId) + '</h2>' +
                '<div id="decompose-tree" style="margin-top:16px;" class="task-tree"></div>' +
                '<div style="margin-top:16px;"><button class="btn btn-primary btn-sm" id="add-subtask-btn">+ Add Sub-task</button></div>' +
                '</div>';

            renderTree(data);
        }).catch(function(err) {
            container.innerHTML = '<div class="empty-state"><div class="empty-icon">❌</div><div class="empty-text">Failed to load: ' + err.message + '</div></div>';
        });
    }

    function renderTree(data) {
        var tree = document.getElementById('decompose-tree');
        if (!tree) return;
        var nodes = data.sub_tasks || data.children || [];
        if (nodes.length === 0) {
            tree.innerHTML = '<div style="color:var(--text-muted);font-size:13px;">No sub-tasks yet</div>';
            return;
        }
        tree.innerHTML = '';
        nodes.forEach(function(n) { renderNode(tree, n, 0); });
    }

    function renderNode(parent, node, depth) {
        var icons = { 'completed': '✅', 'done': '✅', 'running': '🟡', 'in_progress': '🟡', 'queued': '⏳', 'blocked': '🔴' };
        var div = document.createElement('div');
        div.className = 'task-tree-node';
        div.style.paddingLeft = (depth * 20) + 'px';
        div.innerHTML =
            '<span class="task-tree-node-status">' + (icons[(node.status||'').toLowerCase()] || '⏳') + '</span>' +
            '<span class="task-tree-node-text">' + ReefUtils.escapeHtml(node.instruction || node.text || '') + '</span>' +
            '<span class="task-tree-node-assignee">' + ReefUtils.escapeHtml(node.assignee || '') + '</span>';
        parent.appendChild(div);
        (node.children || []).forEach(function(c) { renderNode(parent, c, depth + 1); });
    }

    return {
        render: render, renderSubmit: renderSubmit, renderDecompose: renderDecompose,
        refresh: refresh, cancel: cancel, pause: pause, resume: resume,
        showDetail: showDetail, closeDetail: closeDetail
    };
})();
