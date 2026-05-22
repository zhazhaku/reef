// utils.js — API client, formatters, DOM helpers
'use strict';

var ReefUtils = (function() {

    // ---- API Client ----

    function api(method, path, body) {
        return new Promise(function(resolve, reject) {
            var xhr = new XMLHttpRequest();
            xhr.open(method, path);
            xhr.setRequestHeader('Content-Type', 'application/json');
            xhr.onload = function() {
                try {
                    var data = xhr.responseText ? JSON.parse(xhr.responseText) : null;
                    if (xhr.status >= 200 && xhr.status < 300) {
                        resolve(data);
                    } else {
                        reject(new Error((data && data.error) || 'HTTP ' + xhr.status));
                    }
                } catch (e) {
                    reject(e);
                }
            };
            xhr.onerror = function() { reject(new Error('Network error')); };
            xhr.send(body ? JSON.stringify(body) : null);
        });
    }

    function apiGet(path) { return api('GET', path); }
    function apiPost(path, body) { return api('POST', path, body); }
    function apiPut(path, body) { return api('PUT', path, body); }

    // ---- Formatters ----

    function formatTime(ts) {
        if (!ts) return '--';
        var d = new Date(ts);
        if (isNaN(d.getTime())) return '--';
        var pad = function(n) { return n < 10 ? '0' + n : n; };
        return pad(d.getHours()) + ':' + pad(d.getMinutes()) + ':' + pad(d.getSeconds());
    }

    function formatDateTime(ts) {
        if (!ts) return '--';
        var d = new Date(ts);
        if (isNaN(d.getTime())) return '--';
        var pad = function(n) { return n < 10 ? '0' + n : n; };
        return d.getFullYear() + '-' + pad(d.getMonth()+1) + '-' + pad(d.getDate()) + ' ' +
               pad(d.getHours()) + ':' + pad(d.getMinutes());
    }

    function formatDuration(ms) {
        if (!ms || ms < 0) return '--';
        var s = Math.floor(ms / 1000);
        var m = Math.floor(s / 60);
        var h = Math.floor(m / 60);
        var d = Math.floor(h / 24);
        if (d > 0) return d + 'd ' + (h % 24) + 'h';
        if (h > 0) return h + 'h ' + (m % 60) + 'm';
        if (m > 0) return m + 'm ' + (s % 60) + 's';
        return s + 's';
    }

    function formatBytes(b) {
        if (!b || b < 0) return '0 B';
        var units = ['B', 'KB', 'MB', 'GB'];
        var i = 0;
        while (b >= 1024 && i < units.length - 1) { b /= 1024; i++; }
        return b.toFixed(i > 0 ? 1 : 0) + ' ' + units[i];
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

    // ---- DOM Helpers ----

    function el(tag, cls, text) {
        var e = document.createElement(tag);
        if (cls) e.className = cls;
        if (text) e.textContent = text;
        return e;
    }

    function setText(id, value) {
        var e = document.getElementById(id);
        if (e) e.textContent = value;
    }

    function show(id) {
        var e = document.getElementById(id);
        if (e) e.classList.remove('hidden');
    }

    function hide(id) {
        var e = document.getElementById(id);
        if (e) e.classList.add('hidden');
    }

    function toast(msg, type) {
        type = type || 'info';
        var t = el('div', 'toast ' + type, msg);
        document.body.appendChild(t);
        setTimeout(function() { t.remove(); }, 3000);
    }

    // ---- Status helpers ----

    function stateClass(state) {
        var map = {
            'connected': 'online', 'online': 'online',
            'disconnected': 'offline', 'offline': 'offline',
            'busy': 'busy', 'stale': 'stale',
            'running': 'running', 'queued': 'queued',
            'completed': 'completed', 'failed': 'failed',
            'assigned': 'running', 'cancelled': 'cancelled',
            'escalated': 'escalated'
        };
        return map[(state || '').toLowerCase()] || '';
    }

    function statusBadge(status) {
        return '<span class="badge badge-' + status.toLowerCase() + '">' + escapeHtml(status) + '</span>';
    }

    function priorityBadge(priority) {
        if (!priority) return '';
        return '<span class="badge badge-' + priority.toLowerCase() + '">' + escapeHtml(priority) + '</span>';
    }

    function skillBadges(skills) {
        if (!skills || skills.length === 0) return '--';
        return skills.map(function(s) {
            return '<span class="skill-badge">' + escapeHtml(s) + '</span>';
        }).join('');
    }

    return {
        api: api, apiGet: apiGet, apiPost: apiPost, apiPut: apiPut,
        formatTime: formatTime, formatDateTime: formatDateTime,
        formatDuration: formatDuration, formatBytes: formatBytes,
        truncate: truncate, escapeHtml: escapeHtml,
        el: el, setText: setText, show: show, hide: hide, toast: toast,
        stateClass: stateClass, statusBadge: statusBadge,
        priorityBadge: priorityBadge, skillBadges: skillBadges
    };
})();
