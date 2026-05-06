// board.js — Kanban Board page (stub)
'use strict';

var ReefBoard = (function() {
    var tasks = { backlog: [], in_progress: [], review: [], done: [] };

    function render(container) {
        container.innerHTML = '' +
            '<div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:16px;">' +
                '<div class="filters">' +
                    '<select id="board-filter-role" class="filter-select"><option value="">All Roles</option><option value="coder">Coder</option><option value="tester">Tester</option><option value="reviewer">Reviewer</option><option value="analyst">Analyst</option></select>' +
                '</div>' +
                '<button class="btn btn-primary" onclick="ReefApp.go(\'/tasks/new\')">+ New Task</button>' +
            '</div>' +
            '<div class="kanban" id="kanban-board">' +
                '<div class="kanban-column" data-status="Queued" id="col-backlog">' +
                    '<div class="kanban-column-header"><span class="kanban-column-title">Backlog</span><span class="kanban-column-count" id="count-backlog">0</span></div>' +
                    '<div class="kanban-cards" id="cards-backlog"></div>' +
                '</div>' +
                '<div class="kanban-column" data-status="Running" id="col-in_progress">' +
                    '<div class="kanban-column-header"><span class="kanban-column-title">In Progress</span><span class="kanban-column-count" id="count-in_progress">0</span></div>' +
                    '<div class="kanban-cards" id="cards-in_progress"></div>' +
                '</div>' +
                '<div class="kanban-column" data-status="Assigned" id="col-review">' +
                    '<div class="kanban-column-header"><span class="kanban-column-title">Review</span><span class="kanban-column-count" id="count-review">0</span></div>' +
                    '<div class="kanban-cards" id="cards-review"></div>' +
                '</div>' +
                '<div class="kanban-column" data-status="Completed" id="col-done">' +
                    '<div class="kanban-column-header"><span class="kanban-column-title">Done</span><span class="kanban-column-count" id="count-done">0</span></div>' +
                    '<div class="kanban-cards" id="cards-done"></div>' +
                '</div>' +
            '</div>';

        setupDragDrop();
        refresh();
    }

    function refresh() {
        ReefUtils.apiGet('/api/v2/tasks?limit=200').then(function(data) {
            tasks = { backlog: [], in_progress: [], review: [], done: [] };
            (data.tasks || []).forEach(function(t) {
                if (t.status === 'Queued') tasks.backlog.push(t);
                else if (t.status === 'Running') tasks.in_progress.push(t);
                else if (t.status === 'Assigned') tasks.review.push(t);
                else if (t.status === 'Completed') tasks.done.push(t);
            });
            renderCards();
        }).catch(function() {});
    }

    function renderCards() {
        renderColumn('backlog', tasks.backlog);
        renderColumn('in_progress', tasks.in_progress);
        renderColumn('review', tasks.review);
        renderColumn('done', tasks.done);
    }

    function renderColumn(col, items) {
        var container = document.getElementById('cards-' + col);
        var count = document.getElementById('count-' + col);
        if (!container) return;
        if (count) count.textContent = items.length;
        container.innerHTML = '';
        items.forEach(function(t) {
            var card = document.createElement('div');
            card.className = 'kanban-card';
            card.draggable = true;
            card.setAttribute('data-task-id', t.id);
            card.innerHTML =
                '<div class="kanban-card-title">' + ReefUtils.escapeHtml(ReefUtils.truncate(t.instruction, 80)) + '</div>' +
                '<div class="kanban-card-footer">' +
                    '<span>' + (t.assigned_client ? '🤖 ' + ReefUtils.escapeHtml(t.assigned_client) : '') + '</span>' +
                    '<span>' + ReefUtils.formatTime(t.created_at) + '</span>' +
                '</div>';
            container.appendChild(card);
        });
    }

    function setupDragDrop() {
        document.addEventListener('dragstart', function(e) {
            if (e.target.classList.contains('kanban-card')) {
                e.target.classList.add('dragging');
                e.dataTransfer.setData('text/plain', e.target.getAttribute('data-task-id'));
            }
        });
        document.addEventListener('dragend', function(e) {
            if (e.target.classList.contains('kanban-card')) {
                e.target.classList.remove('dragging');
            }
            document.querySelectorAll('.kanban-column').forEach(function(c) { c.classList.remove('drag-over'); });
        });
        document.addEventListener('dragover', function(e) {
            var col = e.target.closest('.kanban-column');
            if (col) { e.preventDefault(); col.classList.add('drag-over'); }
        });
        document.addEventListener('dragleave', function(e) {
            var col = e.target.closest('.kanban-column');
            if (col) col.classList.remove('drag-over');
        });
        document.addEventListener('drop', function(e) {
            e.preventDefault();
            var col = e.target.closest('.kanban-column');
            if (!col) return;
            col.classList.remove('drag-over');
            var taskId = e.dataTransfer.getData('text/plain');
            var newStatus = col.getAttribute('data-status');
            if (taskId && newStatus) {
                ReefUtils.apiPost('/api/v2/board/move', { task_id: taskId, new_status: newStatus })
                    .then(function() { refresh(); })
                    .catch(function(err) { ReefUtils.toast('Move failed: ' + err.message, 'error'); });
            }
        });
    }

    return { render: render, refresh: refresh };
})();
