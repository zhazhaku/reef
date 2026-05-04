// Reef Dashboard — Vanilla JS SPA
(function() {
    'use strict';

    // State
    let currentPage = 'overview';
    let tasksOffset = 0;
    const tasksLimit = 50;
    let eventSource = null;
    let pollTimer = null;

    // DOM ready
    document.addEventListener('DOMContentLoaded', init);

    function init() {
        setupRouting();
        setupForms();
        setupFilters();
        connectSSE();
        navigate(location.hash || '#/');
    }

    // ---- Routing ----

    function setupRouting() {
        window.addEventListener('hashchange', function() {
            navigate(location.hash);
        });
    }

    function navigate(hash) {
        hash = hash.replace(/^#\/?/, '');
        if (hash === '' || hash === '/') {
            showPage('overview');
        } else if (hash.startsWith('tasks')) {
            showPage('tasks');
            loadTasks();
        } else if (hash.startsWith('clients')) {
            showPage('clients');
            loadClients();
        } else {
            showPage('overview');
        }
    }

    function showPage(name) {
        currentPage = name;
        document.querySelectorAll('.page').forEach(function(p) {
            p.classList.remove('active');
        });
        document.querySelectorAll('.nav-link').forEach(function(l) {
            l.classList.remove('active');
        });

        var page = document.getElementById('page-' + name);
        if (page) page.classList.add('active');

        var link = document.querySelector('.nav-link[data-page="' + name + '"]');
        if (link) link.classList.add('active');
    }

    // ---- SSE ----

    function connectSSE() {
        if (eventSource) {
            eventSource.close();
        }

        var dot = document.getElementById('sse-status');
        var label = document.getElementById('sse-label');

        eventSource = new EventSource('/api/v2/events');

        eventSource.onopen = function() {
            dot.className = 'status-dot connected';
            label.textContent = 'Connected';
            if (pollTimer) {
                clearInterval(pollTimer);
                pollTimer = null;
            }
        };

        eventSource.addEventListener('stats_update', function(e) {
            try {
                var data = JSON.parse(e.data);
                updateStats(data);
            } catch (err) {}
        });

        eventSource.addEventListener('task_update', function(e) {
            if (currentPage === 'tasks') {
                loadTasks();
            }
        });

        eventSource.addEventListener('client_update', function(e) {
            if (currentPage === 'clients') {
                loadClients();
            }
        });

        eventSource.onerror = function() {
            dot.className = 'status-dot error';
            label.textContent = 'Reconnecting...';
            eventSource.close();
            eventSource = null;
            startPolling();
        };
    }

    function startPolling() {
        if (pollTimer) return;
        pollTimer = setInterval(function() {
            loadStatus();
            if (currentPage === 'tasks') loadTasks();
            if (currentPage === 'clients') loadClients();
        }, 5000);
        // Immediate first poll
        loadStatus();
    }

    // ---- API ----

    function apiGet(url, callback) {
        var xhr = new XMLHttpRequest();
        xhr.open('GET', url);
        xhr.onload = function() {
            if (xhr.status >= 200 && xhr.status < 300) {
                try {
                    callback(null, JSON.parse(xhr.responseText));
                } catch (e) {
                    callback(e, null);
                }
            } else {
                callback(new Error('HTTP ' + xhr.status), null);
            }
        };
        xhr.onerror = function() {
            callback(new Error('Network error'), null);
        };
        xhr.send();
    }

    function apiPost(url, body, callback) {
        var xhr = new XMLHttpRequest();
        xhr.open('POST', url);
        xhr.setRequestHeader('Content-Type', 'application/json');
        xhr.onload = function() {
            try {
                var data = JSON.parse(xhr.responseText);
                if (xhr.status >= 200 && xhr.status < 300) {
                    callback(null, data);
                } else {
                    callback(new Error(data.error || 'HTTP ' + xhr.status), data);
                }
            } catch (e) {
                callback(e, null);
            }
        };
        xhr.onerror = function() {
            callback(new Error('Network error'), null);
        };
        xhr.send(JSON.stringify(body));
    }

    // ---- Status ----

    function loadStatus() {
        apiGet('/api/v2/status', function(err, data) {
            if (err || !data) return;
            updateStats(data);
        });
    }

    function updateStats(data) {
        setText('stat-uptime', formatUptime(data.uptime_ms));
        setText('stat-clients', data.connected_clients || 0);
        if (data.task_stats) {
            setText('stat-queued', data.task_stats.queued || 0);
            setText('stat-running', data.task_stats.running || 0);
            setText('stat-completed', data.task_stats.completed || 0);
            setText('stat-failed', data.task_stats.failed || 0);
        }
    }

    // ---- Tasks ----

    function loadTasks() {
        var status = document.getElementById('filter-status').value;
        var role = document.getElementById('filter-role').value;
        var url = '/api/v2/tasks?limit=' + tasksLimit + '&offset=' + tasksOffset;
        if (status) url += '&status=' + encodeURIComponent(status);
        if (role) url += '&role=' + encodeURIComponent(role);

        apiGet(url, function(err, data) {
            if (err || !data) return;
            renderTasks(data);
        });
    }

    function renderTasks(data) {
        var tbody = document.getElementById('tasks-body');
        tbody.innerHTML = '';

        if (!data.tasks || data.tasks.length === 0) {
            tbody.innerHTML = '<tr><td colspan="7" style="text-align:center;color:var(--text-muted);padding:24px;">No tasks found</td></tr>';
            renderPagination(data.total, data.limit, data.offset);
            return;
        }

        data.tasks.forEach(function(task) {
            var tr = document.createElement('tr');
            var started = task.started_at ? formatTime(task.started_at) : '--';
            var badgeClass = 'badge-' + task.status.toLowerCase();
            var actions = '';
            if (task.status === 'Queued' || task.status === 'Running') {
                actions = '<button class="btn btn-danger btn-sm" onclick="window.__cancelTask(\'' + task.id + '\')">Cancel</button>';
            }

            tr.innerHTML =
                '<td class="mono">' + escapeHtml(task.id) + '</td>' +
                '<td><span class="badge ' + badgeClass + '">' + escapeHtml(task.status) + '</span></td>' +
                '<td>' + escapeHtml(task.required_role) + '</td>' +
                '<td>' + escapeHtml(truncate(task.instruction, 60)) + '</td>' +
                '<td class="mono">' + escapeHtml(task.assigned_client || '--') + '</td>' +
                '<td>' + formatTime(task.created_at) + '</td>' +
                '<td>' + actions + '</td>';
            tbody.appendChild(tr);
        });

        renderPagination(data.total, data.limit, data.offset);
    }

    function renderPagination(total, limit, offset) {
        var container = document.getElementById('tasks-pagination');
        container.innerHTML = '';

        if (total <= limit) return;

        var totalPages = Math.ceil(total / limit);
        var currentPageNum = Math.floor(offset / limit) + 1;

        var prevBtn = document.createElement('button');
        prevBtn.textContent = '← Prev';
        prevBtn.disabled = offset === 0;
        prevBtn.onclick = function() {
            tasksOffset = Math.max(0, offset - limit);
            loadTasks();
        };

        var info = document.createElement('span');
        info.className = 'page-info';
        info.textContent = 'Page ' + currentPageNum + ' of ' + totalPages + ' (' + total + ' total)';

        var nextBtn = document.createElement('button');
        nextBtn.textContent = 'Next →';
        nextBtn.disabled = offset + limit >= total;
        nextBtn.onclick = function() {
            tasksOffset = offset + limit;
            loadTasks();
        };

        container.appendChild(prevBtn);
        container.appendChild(info);
        container.appendChild(nextBtn);
    }

    window.__cancelTask = function(taskId) {
        if (!confirm('Cancel task ' + taskId + '?')) return;
        // Use the admin API to cancel — POST to /tasks won't work for cancel
        // For now, just show a message
        alert('Task cancellation not yet implemented in the v2 API');
    };

    // ---- Clients ----

    function loadClients() {
        apiGet('/api/v2/clients', function(err, data) {
            if (err || !data) return;
            renderClients(data);
        });
    }

    function renderClients(clients) {
        var tbody = document.getElementById('clients-body');
        tbody.innerHTML = '';

        if (!clients || clients.length === 0) {
            tbody.innerHTML = '<tr><td colspan="6" style="text-align:center;color:var(--text-muted);padding:24px;">No clients connected</td></tr>';
            return;
        }

        clients.forEach(function(client) {
            var tr = document.createElement('tr');
            var badgeClass = 'badge-' + client.state.toLowerCase();
            var skills = (client.skills || []).join(', ') || '--';

            tr.innerHTML =
                '<td class="mono">' + escapeHtml(client.id) + '</td>' +
                '<td>' + escapeHtml(client.role) + '</td>' +
                '<td>' + escapeHtml(skills) + '</td>' +
                '<td><span class="badge ' + badgeClass + '">' + escapeHtml(client.state) + '</span></td>' +
                '<td>' + client.load + '</td>' +
                '<td>' + formatTime(client.last_heartbeat) + '</td>';
            tbody.appendChild(tr);
        });
    }

    // ---- Forms ----

    function setupForms() {
        var form = document.getElementById('task-form');
        if (form) {
            form.addEventListener('submit', function(e) {
                e.preventDefault();
                submitTask();
            });
        }
    }

    function submitTask() {
        var instruction = document.getElementById('instruction').value.trim();
        var role = document.getElementById('required_role').value.trim();
        var skillsStr = document.getElementById('required_skills').value.trim();
        var skills = skillsStr ? skillsStr.split(',').map(function(s) { return s.trim(); }).filter(Boolean) : [];

        if (!instruction || !role) {
            showSubmitResult('Instruction and role are required', true);
            return;
        }

        apiPost('/tasks', {
            instruction: instruction,
            required_role: role,
            required_skills: skills
        }, function(err, data) {
            if (err) {
                showSubmitResult(err.message, true);
            } else {
                showSubmitResult('Task submitted: ' + data.task_id, false);
                document.getElementById('task-form').reset();
            }
        });
    }

    function showSubmitResult(msg, isError) {
        var el = document.getElementById('submit-result');
        el.textContent = msg;
        el.className = 'submit-result ' + (isError ? 'error' : 'success');
        setTimeout(function() {
            el.className = 'submit-result hidden';
        }, 5000);
    }

    // ---- Filters ----

    function setupFilters() {
        var statusFilter = document.getElementById('filter-status');
        var roleFilter = document.getElementById('filter-role');
        var refreshBtn = document.getElementById('btn-refresh-tasks');
        var refreshClientsBtn = document.getElementById('btn-refresh-clients');

        if (statusFilter) {
            statusFilter.addEventListener('change', function() {
                tasksOffset = 0;
                loadTasks();
            });
        }
        if (roleFilter) {
            var debounce;
            roleFilter.addEventListener('input', function() {
                clearTimeout(debounce);
                debounce = setTimeout(function() {
                    tasksOffset = 0;
                    loadTasks();
                }, 300);
            });
        }
        if (refreshBtn) {
            refreshBtn.addEventListener('click', function() {
                tasksOffset = 0;
                loadTasks();
            });
        }
        if (refreshClientsBtn) {
            refreshClientsBtn.addEventListener('click', loadClients);
        }
    }

    // ---- Helpers ----

    function setText(id, value) {
        var el = document.getElementById(id);
        if (el) el.textContent = value;
    }

    function formatUptime(ms) {
        if (!ms || ms < 0) return '--';
        var seconds = Math.floor(ms / 1000);
        var minutes = Math.floor(seconds / 60);
        var hours = Math.floor(minutes / 60);
        var days = Math.floor(hours / 24);

        if (days > 0) return days + 'd ' + (hours % 24) + 'h';
        if (hours > 0) return hours + 'h ' + (minutes % 60) + 'm';
        if (minutes > 0) return minutes + 'm ' + (seconds % 60) + 's';
        return seconds + 's';
    }

    function formatTime(ts) {
        if (!ts) return '--';
        var d = new Date(ts);
        if (isNaN(d.getTime())) return '--';
        var pad = function(n) { return n < 10 ? '0' + n : n; };
        return pad(d.getHours()) + ':' + pad(d.getMinutes()) + ':' + pad(d.getSeconds());
    }

    function truncate(str, len) {
        if (!str) return '';
        return str.length > len ? str.substring(0, len) + '...' : str;
    }

    function escapeHtml(str) {
        if (!str) return '';
        var div = document.createElement('div');
        div.textContent = str;
        return div.innerHTML;
    }
})();
