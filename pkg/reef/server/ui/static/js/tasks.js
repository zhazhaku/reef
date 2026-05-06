// tasks.js — Tasks list, submit, decompose
'use strict';

var ReefTasks = (function() {
    var offset = 0;
    var limit = 50;

    function render(container) {
        container.innerHTML = '' +
            '<div class="card">' +
                '<div class="card-header">' +
                    '<h2>Tasks</h2>' +
                    '<div class="filters">' +
                        '<select id="task-filter-status" class="filter-select"><option value="">All Statuses</option>' +
                            '<option value="Queued">Queued</option><option value="Running">Running</option><option value="Completed">Completed</option>' +
                            '<option value="Failed">Failed</option><option value="Cancelled">Cancelled</option><option value="Escalated">Escalated</option></select>' +
                        '<input type="text" id="task-filter-role" class="filter-input" placeholder="Filter by role...">' +
                        '<button class="btn btn-primary" onclick="ReefApp.go(\'/tasks/new\')">+ New Task</button>' +
                    '</div>' +
                '</div>' +
                '<div class="table-container"><table class="data-table"><thead><tr>' +
                    '<th>ID</th><th>Status</th><th>Role</th><th>Instruction</th><th>Assigned</th><th>Created</th><th>Actions</th>' +
                '</tr></thead><tbody id="tasks-body"></tbody></table></div>' +
                '<div class="pagination" id="tasks-pagination"></div>' +
            '</div>';

        document.getElementById('task-filter-status').addEventListener('change', function() { offset = 0; loadTasks(); });
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
                tbody.innerHTML = '<tr><td colspan="7" style="text-align:center;color:var(--text-muted);padding:24px;">No tasks found</td></tr>';
                return;
            }
            tbody.innerHTML = '';
            data.tasks.forEach(function(t) {
                var tr = document.createElement('tr');
                var actions = '';
                if (t.status === 'Queued' || t.status === 'Running') {
                    actions = '<button class="btn btn-danger btn-sm" onclick="ReefTasks.cancel(\'' + t.id + '\')">Cancel</button>';
                }
                tr.innerHTML =
                    '<td class="mono">' + ReefUtils.escapeHtml(t.id) + '</td>' +
                    '<td>' + ReefUtils.statusBadge(t.status) + '</td>' +
                    '<td>' + ReefUtils.escapeHtml(t.required_role) + '</td>' +
                    '<td>' + ReefUtils.escapeHtml(ReefUtils.truncate(t.instruction, 60)) + '</td>' +
                    '<td class="mono">' + ReefUtils.escapeHtml(t.assigned_client || '--') + '</td>' +
                    '<td>' + ReefUtils.formatTime(t.created_at) + '</td>' +
                    '<td>' + actions + '</td>';
                tbody.appendChild(tr);
            });
            renderPagination(data.total, data.limit, data.offset);
        }).catch(function() {});
    }

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

    function cancel(taskId) {
        if (!confirm('Cancel task ' + taskId + '?')) return;
        ReefUtils.apiPost('/api/v2/tasks/' + taskId + '/cancel').then(function() {
            ReefUtils.toast('Task cancelled', 'info');
            loadTasks();
        }).catch(function(e) { ReefUtils.toast(e.message, 'error'); });
    }

    return { render: render, renderSubmit: renderSubmit, renderDecompose: renderDecompose, refresh: refresh, cancel: cancel };
})();
