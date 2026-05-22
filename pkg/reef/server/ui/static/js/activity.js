// activity.js — Activity Timeline page
'use strict';

var ReefActivity = (function() {
    var events = [];
    var filter = '';

    function render(container) {
        container.innerHTML = '' +
            '<div class="card">' +
                '<div class="card-header">' +
                    '<h2>Activity Timeline</h2>' +
                    '<div class="filters">' +
                        '<select id="activity-filter" class="filter-select">' +
                            '<option value="">All</option><option value="agent">Agent</option><option value="task">Task</option><option value="evolution">Evolution</option><option value="system">System</option>' +
                        '</select>' +
                        '<input type="text" id="activity-search" class="filter-input" placeholder="Search...">' +
                        '<button class="btn btn-secondary btn-sm" id="activity-export">📥 Export</button>' +
                    '</div>' +
                '</div>' +
                '<div id="activity-list"></div>' +
                '<div style="text-align:center;margin-top:16px;"><button class="btn btn-secondary btn-sm" id="activity-load-more">Load More</button></div>' +
            '</div>';

        document.getElementById('activity-filter').addEventListener('change', function() { filter = this.value; renderList(); });
        document.getElementById('activity-search').addEventListener('input', function() { renderList(); });
        document.getElementById('activity-export').addEventListener('click', exportEvents);
        document.getElementById('activity-load-more').addEventListener('click', loadEvents);
        loadEvents();
    }

    function loadEvents() {
        var url = '/api/v2/activity?limit=50';
        if (filter) url += '&type=' + filter;
        ReefUtils.apiGet(url).then(function(data) {
            events = data.events || data || [];
            renderList();
        }).catch(function() {});
    }

    function renderList() {
        var container = document.getElementById('activity-list');
        if (!container) return;
        var search = (document.getElementById('activity-search') || {}).value || '';
        var filtered = events.filter(function(e) {
            if (filter && e.type !== filter) return false;
            if (search && !(e.description || '').toLowerCase().includes(search.toLowerCase())) return false;
            return true;
        });

        if (filtered.length === 0) {
            container.innerHTML = '<div class="empty-state"><div class="empty-icon">📰</div><div class="empty-text">No activity events</div></div>';
            return;
        }

        var icons = { agent: '🟢', task: '📋', evolution: '🧬', system: '⚡' };
        container.innerHTML = filtered.map(function(e) {
            return '<div class="activity-item">' +
                '<span class="activity-time">' + ReefUtils.formatTime(e.timestamp) + '</span>' +
                '<span class="activity-icon">' + (icons[e.type] || '📌') + '</span>' +
                '<span class="activity-text">' + ReefUtils.escapeHtml(e.description) + '</span>' +
            '</div>';
        }).join('');
    }

    function onEvent(data) {
        events.unshift(data);
        renderList();
    }

    function exportEvents() {
        var blob = new Blob([JSON.stringify(events, null, 2)], { type: 'application/json' });
        var a = document.createElement('a');
        a.href = URL.createObjectURL(blob);
        a.download = 'activity-' + new Date().toISOString().slice(0, 10) + '.json';
        a.click();
    }

    return { render: render, onEvent: onEvent };
})();
