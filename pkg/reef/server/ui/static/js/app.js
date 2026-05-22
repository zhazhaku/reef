// app.js — SPA Router, Global State, SSE, Theme
'use strict';

var ReefApp = (function() {
    var state = {
        theme: localStorage.getItem('reef-theme') || 'dark',
        currentPage: '',
        sseConnected: false,
        hermesMode: 'coordinator'
    };

    var eventSource = null;
    var reconnectDelay = 1000;
    var maxReconnectDelay = 30000;
    var handlers = {}; // page-specific SSE handlers

    // ---- Router ----
    var routes = {
        '': 'dashboard',
        '/': 'dashboard',
        '/board': 'board',
        '/chatroom': 'chatroom',
        '/clients': 'clients',
        '/clients/detail': 'clientDetail',
        '/tasks': 'tasks',
        '/tasks/new': 'taskSubmit',
        '/tasks/decompose': 'taskDecompose',
        '/evolution': 'evolution',
        '/hermes': 'hermes',
        '/config': 'config',
        '/monitoring': 'monitoring',
        '/activity': 'activity'
    };

    function init() {
        applyTheme(state.theme);
        setupRouting();
        setupSidebar();
        setupThemeToggle();
        connectSSE();
        navigate(location.hash || '#/');
    }

    function setupRouting() {
        window.addEventListener('hashchange', function() {
            navigate(location.hash);
        });
    }

    function navigate(hash) {
        var path = hash.replace(/^#/, '') || '/';
        var basePath = path.split('?')[0].split('/')[1] ? '/' + path.split('/')[1] : path;
        // Handle parameterized routes
        var segments = path.split('/').filter(Boolean);
        var routeKey = '/' + (segments[0] || '');
        var param = segments[1] || '';

        var page = routes[routeKey] || 'dashboard';
        state.currentPage = page;

        // Update sidebar active state
        document.querySelectorAll('.nav-item').forEach(function(item) {
            item.classList.toggle('active', item.getAttribute('data-page') === page ||
                item.getAttribute('href') === '#' + routeKey);
        });

        // Update topbar breadcrumb
        var breadcrumb = document.getElementById('topbar-breadcrumb');
        if (breadcrumb) {
            var titles = {
                'dashboard': 'Dashboard', 'board': 'Team Board', 'chatroom': 'Team Chatroom',
                'clients': 'Clients', 'clientDetail': 'Client Detail', 'tasks': 'Tasks',
                'taskSubmit': 'Submit Task', 'taskDecompose': 'Task Decomposition',
                'evolution': 'Evolution', 'hermes': 'Hermes Configuration',
                'config': 'System Configuration', 'monitoring': 'Monitoring', 'activity': 'Activity'
            };
            breadcrumb.textContent = titles[page] || 'Dashboard';
        }

        // Render page
        renderPage(page, param);
    }

    function renderPage(page, param) {
        var content = document.getElementById('page-content');
        if (!content) return;
        content.innerHTML = '';

        switch (page) {
            case 'dashboard':
                if (typeof ReefDashboard !== 'undefined') ReefDashboard.render(content);
                break;
            case 'board':
                if (typeof ReefBoard !== 'undefined') ReefBoard.render(content);
                break;
            case 'chatroom':
                if (typeof ReefChatroom !== 'undefined') ReefChatroom.render(content, param);
                break;
            case 'clients':
                if (typeof ReefClients !== 'undefined') ReefClients.render(content);
                break;
            case 'clientDetail':
                if (typeof ReefClients !== 'undefined') ReefClients.renderDetail(content, param);
                break;
            case 'tasks':
                if (typeof ReefTasks !== 'undefined') ReefTasks.render(content);
                break;
            case 'taskSubmit':
                if (typeof ReefTasks !== 'undefined') ReefTasks.renderSubmit(content);
                break;
            case 'taskDecompose':
                if (typeof ReefTasks !== 'undefined') ReefTasks.renderDecompose(content, param);
                break;
            case 'evolution':
                if (typeof ReefEvolution !== 'undefined') ReefEvolution.render(content);
                break;
            case 'hermes':
                if (typeof ReefHermes !== 'undefined') ReefHermes.render(content);
                break;
            case 'config':
                if (typeof ReefConfig !== 'undefined') ReefConfig.render(content);
                break;
            case 'monitoring':
                if (typeof ReefMonitoring !== 'undefined') ReefMonitoring.render(content);
                break;
            case 'activity':
                if (typeof ReefActivity !== 'undefined') ReefActivity.render(content);
                break;
            default:
                content.innerHTML = '<div class="empty-state"><div class="empty-icon">🚧</div><div class="empty-text">Page not found</div></div>';
        }
    }

    // ---- Sidebar ----
    function setupSidebar() {
        var hamburger = document.getElementById('hamburger');
        var sidebar = document.getElementById('sidebar');
        var overlay = document.getElementById('sidebar-overlay');

        if (hamburger) {
            hamburger.addEventListener('click', function() {
                sidebar.classList.toggle('open');
                overlay.classList.toggle('open');
            });
        }
        if (overlay) {
            overlay.addEventListener('click', function() {
                sidebar.classList.remove('open');
                overlay.classList.remove('open');
            });
        }

        // Nav item clicks
        document.querySelectorAll('.nav-item').forEach(function(item) {
            item.addEventListener('click', function() {
                // Close mobile sidebar
                if (sidebar) sidebar.classList.remove('open');
                if (overlay) overlay.classList.remove('open');
            });
        });
    }

    // ---- Theme ----
    function setupThemeToggle() {
        var btn = document.getElementById('theme-toggle');
        if (btn) {
            btn.addEventListener('click', function() {
                state.theme = state.theme === 'dark' ? 'light' : 'dark';
                applyTheme(state.theme);
                localStorage.setItem('reef-theme', state.theme);
            });
        }
    }

    function applyTheme(theme) {
        document.documentElement.setAttribute('data-theme', theme);
        var btn = document.getElementById('theme-toggle');
        if (btn) btn.textContent = theme === 'dark' ? '☀️' : '🌙';
    }

    // ---- SSE ----
    function connectSSE() {
        if (eventSource) eventSource.close();

        var dot = document.getElementById('sse-dot');
        var label = document.getElementById('sse-label');

        eventSource = new EventSource('/api/v2/events');

        eventSource.onopen = function() {
            state.sseConnected = true;
            reconnectDelay = 1000;
            if (dot) { dot.className = 'sse-dot connected'; }
            if (label) label.textContent = 'Connected';
        };

        eventSource.addEventListener('stats_update', function(e) {
            try {
                var data = JSON.parse(e.data);
                if (typeof ReefDashboard !== 'undefined' && state.currentPage === 'dashboard') {
                    ReefDashboard.updateStats(data);
                }
            } catch (err) {}
        });

        eventSource.addEventListener('task_update', function(e) {
            try {
                var data = JSON.parse(e.data);
                if (typeof ReefBoard !== 'undefined' && state.currentPage === 'board') {
                    ReefBoard.refresh();
                }
                if (typeof ReefTasks !== 'undefined' && state.currentPage === 'tasks') {
                    ReefTasks.refresh();
                }
            } catch (err) {}
        });

        eventSource.addEventListener('client_update', function(e) {
            try {
                var data = JSON.parse(e.data);
                if (typeof ReefClients !== 'undefined' && state.currentPage === 'clients') {
                    ReefClients.refresh();
                }
            } catch (err) {}
        });

        eventSource.addEventListener('board_update', function(e) {
            if (typeof ReefBoard !== 'undefined' && state.currentPage === 'board') {
                ReefBoard.refresh();
            }
        });

        eventSource.addEventListener('chatroom_message', function(e) {
            if (typeof ReefChatroom !== 'undefined' && state.currentPage === 'chatroom') {
                ReefChatroom.onMessage(JSON.parse(e.data));
            }
        });

        eventSource.addEventListener('chat_message', function(e) {
            try {
                if (typeof ReefChatroom !== 'undefined' && state.currentPage === 'chatroom') {
                    ReefChatroom.onMessage(JSON.parse(e.data));
                }
            } catch (err) {}
        });

        eventSource.addEventListener('evolution_update', function(e) {
            if (typeof ReefEvolution !== 'undefined' && state.currentPage === 'evolution') {
                ReefEvolution.refresh();
            }
        });

        eventSource.addEventListener('activity_event', function(e) {
            if (typeof ReefActivity !== 'undefined' && state.currentPage === 'activity') {
                ReefActivity.onEvent(JSON.parse(e.data));
            }
        });

        eventSource.onerror = function() {
            state.sseConnected = false;
            if (dot) dot.className = 'sse-dot error';
            if (label) label.textContent = 'Reconnecting...';
            eventSource.close();
            eventSource = null;
            setTimeout(connectSSE, reconnectDelay);
            reconnectDelay = Math.min(reconnectDelay * 2, maxReconnectDelay);
        };
    }

    // ---- Public API ----
    return {
        init: init,
        navigate: navigate,
        state: state,
        go: function(path) { location.hash = '#' + path; }
    };
})();

// Boot
document.addEventListener('DOMContentLoaded', ReefApp.init);
