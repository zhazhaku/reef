// board.js — Kanban Board with drag-drop, role filtering, and live updates
'use strict';

var ReefBoard = (function() {
    'use strict';

    // ---- State ----
    var boardData = { backlog: [], in_progress: [], review: [], done: [] };
    var selectedRole = '';
    var allRoles = [];

    // Column definitions: column key → display title, data-status for move API
    var COLUMNS = [
        { key: 'backlog',     title: 'Backlog',     status: 'Queued',    icon: '📋' },
        { key: 'in_progress',  title: 'In Progress',  status: 'Running',   icon: '⚡' },
        { key: 'review',       title: 'Review',       status: 'Review',    icon: '🔍' },
        { key: 'done',         title: 'Done',         status: 'Completed', icon: '✅' }
    ];

    // ---- Render ----
    function render(container) {
        container.innerHTML = buildHTML();
        loadRoles();
        refresh();
    }

    function buildHTML() {
        var html = '' +
            '<div class="board-toolbar">' +
                '<div class="filters">' +
                    '<select id="board-filter-role" class="filter-select">' +
                        '<option value="">All Roles</option>' +
                    '</select>' +
                '</div>' +
                '<button class="btn btn-primary" onclick="ReefApp.go(\'/tasks/new\')">+ New Task</button>' +
            '</div>' +
            '<div class="kanban" id="kanban-board">';

        COLUMNS.forEach(function(col) {
            html += '' +
                '<div class="kanban-column" data-status="' + col.status + '" id="col-' + col.key + '">' +
                    '<div class="kanban-column-header">' +
                        '<span class="kanban-column-title">' + col.icon + ' ' + col.title + '</span>' +
                        '<span class="kanban-column-count badge badge-secondary" id="count-' + col.key + '">0</span>' +
                    '</div>' +
                    '<div class="kanban-cards" id="cards-' + col.key + '">' +
                        '<div class="kanban-empty">No tasks</div>' +
                    '</div>' +
                '</div>';
        });

        html += '</div>';

        setupDragDrop();
        return html;
    }

    // ---- Data Loading ----
    function loadRoles() {
        ReefUtils.apiGet('/api/v2/clients').then(function(clients) {
            var roles = {};
            (clients || []).forEach(function(c) {
                if (c.role) roles[c.role] = true;
            });
            allRoles = Object.keys(roles).sort();
            populateRoleFilter();
        }).catch(function() {
            // Role filter will remain as "All Roles" only
        });
    }

    function populateRoleFilter() {
        var sel = document.getElementById('board-filter-role');
        if (!sel) return;

        // Keep the "All Roles" option
        var currentVal = sel.value;

        // Remove existing dynamic options (keep first option)
        while (sel.options.length > 1) {
            sel.remove(1);
        }

        allRoles.forEach(function(role) {
            var opt = document.createElement('option');
            opt.value = role;
            opt.textContent = role;
            sel.appendChild(opt);
        });

        sel.value = currentVal;

        // Bind filter change
        sel.onchange = function() {
            selectedRole = sel.value;
            renderAllColumns();
        };
    }

    function refresh() {
        ReefUtils.apiGet('/api/v2/board').then(function(data) {
            boardData = {
                backlog:     data.backlog || [],
                in_progress:  data.in_progress || [],
                review:       data.review || [],
                done:         data.done || []
            };
            renderAllColumns();
        }).catch(function(err) {
            showBoardError(err);
        });
    }

    // ---- Column Rendering ----
    function renderAllColumns() {
        COLUMNS.forEach(function(col) {
            renderColumn(col);
        });
    }

    function renderColumn(col) {
        var container = document.getElementById('cards-' + col.key);
        var countEl   = document.getElementById('count-' + col.key);
        if (!container) return;

        var items = boardData[col.key] || [];

        // Apply role filter
        if (selectedRole) {
            items = items.filter(function(t) {
                return (t.required_role || t.role || '') === selectedRole;
            });
        }

        // Update count badge
        if (countEl) countEl.textContent = items.length;

        // Empty state
        if (items.length === 0) {
            container.innerHTML = '<div class="kanban-empty">No tasks</div>';
            return;
        }

        // Render cards
        container.innerHTML = '';
        items.forEach(function(t) {
            var card = buildCard(t);
            container.appendChild(card);
        });
    }

    function buildCard(task) {
        var card = document.createElement('div');
        card.className = 'kanban-card';
        card.draggable = true;
        card.setAttribute('data-task-id', task.id || '');
        card.setAttribute('data-status', task.status || '');

        // Click to navigate to task decomposition
        card.addEventListener('click', function(e) {
            // Don't navigate if dragging was in progress
            if (card.classList.contains('dragging')) return;
            ReefApp.go('/tasks/decompose/' + encodeURIComponent(task.id || ''));
        });

        // Priority badge
        var priorityHtml = '';
        if (task.priority) {
            var priClass = 'badge-priority-' + task.priority.toLowerCase();
            priorityHtml = '<span class="badge badge-priority ' + priClass + '">' + ReefUtils.escapeHtml(task.priority) + '</span>';
        }

        // Agent avatar (first letter of assigned client)
        var avatarHtml = '';
        if (task.assigned_client) {
            var initial = task.assigned_client.charAt(0).toUpperCase();
            avatarHtml = '<span class="agent-avatar-sm" title="' + ReefUtils.escapeHtml(task.assigned_client) + '">' + initial + '</span>';
        }

        // Role tag
        var roleHtml = '';
        var role = task.required_role || task.role || '';
        if (role) {
            roleHtml = '<span class="kanban-card-role">' + ReefUtils.escapeHtml(role) + '</span>';
        }

        // Created time
        var timeHtml = '<span class="kanban-card-time">' + ReefUtils.formatTime(task.created_at) + '</span>';

        card.innerHTML = '' +
            '<div class="kanban-card-header-row">' +
                priorityHtml +
                roleHtml +
                avatarHtml +
            '</div>' +
            '<div class="kanban-card-title" title="' + ReefUtils.escapeHtml(task.instruction || '') + '">' +
                ReefUtils.escapeHtml(ReefUtils.truncate(task.instruction, 80)) +
            '</div>' +
            '<div class="kanban-card-footer">' +
                timeHtml +
                '<span class="kanban-card-id mono">#' + ReefUtils.escapeHtml(ReefUtils.truncate(task.id, 12)) + '</span>' +
            '</div>';

        return card;
    }

    // ---- Drag and Drop ----
    function setupDragDrop() {
        // Delegated event listeners on the document
        document.addEventListener('dragstart', onDragStart);
        document.addEventListener('dragend',   onDragEnd);
        document.addEventListener('dragover',  onDragOver);
        document.addEventListener('dragleave', onDragLeave);
        document.addEventListener('drop',      onDrop);
    }

    function onDragStart(e) {
        var card = e.target.closest('.kanban-card');
        if (!card) return;
        card.classList.add('dragging');
        e.dataTransfer.setData('text/plain', card.getAttribute('data-task-id') || '');
        e.dataTransfer.effectAllowed = 'move';
    }

    function onDragEnd(e) {
        var card = e.target.closest('.kanban-card');
        if (card) card.classList.remove('dragging');
        document.querySelectorAll('.kanban-column').forEach(function(c) {
            c.classList.remove('drag-over');
        });
    }

    function onDragOver(e) {
        var col = e.target.closest('.kanban-column');
        if (!col) return;
        e.preventDefault();
        e.dataTransfer.dropEffect = 'move';
        col.classList.add('drag-over');
    }

    function onDragLeave(e) {
        var col = e.target.closest('.kanban-column');
        if (!col) return;
        // Check if we actually left the column (not just entering a child)
        var related = e.relatedTarget;
        if (!related || !col.contains(related)) {
            col.classList.remove('drag-over');
        }
    }

    function onDrop(e) {
        e.preventDefault();
        var col = e.target.closest('.kanban-column');
        if (!col) return;
        col.classList.remove('drag-over');

        var taskId = e.dataTransfer.getData('text/plain');
        var newStatus = col.getAttribute('data-status');
        if (!taskId || !newStatus) return;

        // Optimistic UI update
        moveTaskOptimistic(taskId, newStatus);

        // API call
        ReefUtils.apiPost('/api/v2/board/move', { task_id: taskId, new_status: newStatus })
            .then(function() {
                // Refresh to sync with server state
                refresh();
            })
            .catch(function(err) {
                ReefUtils.toast('Move failed: ' + (err.message || 'unknown error'), 'error');
                refresh(); // Revert to server state
            });
    }

    function moveTaskOptimistic(taskId, newStatus) {
        // Find which column maps to newStatus and move the task there
        var targetCol = null;
        COLUMNS.forEach(function(col) {
            if (col.status === newStatus) targetCol = col.key;
        });
        if (!targetCol) return;

        // Remove from all columns
        var movedTask = null;
        COLUMNS.forEach(function(col) {
            var items = boardData[col.key];
            for (var i = 0; i < items.length; i++) {
                if (items[i].id === taskId) {
                    movedTask = items.splice(i, 1)[0];
                    break;
                }
            }
        });

        // Add to target column
        if (movedTask) {
            movedTask.status = newStatus;
            boardData[targetCol].push(movedTask);
        }

        renderAllColumns();
    }

    // ---- Error Handling ----
    function showBoardError(err) {
        var board = document.getElementById('kanban-board');
        if (!board) return;
        board.innerHTML = '' +
            '<div class="empty-state" style="grid-column:1/-1;padding:40px;">' +
                '<div class="empty-icon">🔌</div>' +
                '<div class="empty-text">Unable to load board</div>' +
                '<div style="font-size:12px;color:var(--text-muted);margin-top:4px;">' +
                    ReefUtils.escapeHtml(err.message || 'Server unreachable') +
                '</div>' +
                '<button class="btn btn-secondary btn-sm" style="margin-top:12px;" onclick="ReefBoard.refresh()">Retry</button>' +
            '</div>';
    }

    // ---- Public API ----
    return {
        render: render,
        refresh: refresh
    };
})();
