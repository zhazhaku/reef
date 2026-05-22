// hermes.js — Hermes Configuration page
'use strict';

var ReefHermes = (function() {
    var tools = ['reef_submit', 'reef_query', 'reef_status', 'message', 'reaction', 'cron', 'web_search', 'web_fetch', 'exec', 'read_file', 'write_file'];

    function render(container) {
        container.innerHTML = '' +
            '<div class="card">' +
                '<h2>Hermes Mode</h2>' +
                '<div style="display:flex;gap:16px;margin-top:12px;">' +
                    '<label style="display:flex;align-items:center;gap:6px;cursor:pointer;"><input type="radio" name="hermes-mode" value="full"> Full</label>' +
                    '<label style="display:flex;align-items:center;gap:6px;cursor:pointer;"><input type="radio" name="hermes-mode" value="coordinator" checked> Coordinator</label>' +
                    '<label style="display:flex;align-items:center;gap:6px;cursor:pointer;"><input type="radio" name="hermes-mode" value="executor"> Executor</label>' +
                '</div>' +
            '</div>' +
            '<div class="card">' +
                '<h2>Fallback Configuration</h2>' +
                '<div style="display:flex;align-items:center;gap:12px;margin-top:12px;">' +
                    '<label class="toggle"><input type="checkbox" id="hermes-fallback" checked><span class="toggle-slider"></span></label>' +
                    '<span style="font-size:13px;">Enable Fallback</span>' +
                    '<input type="number" id="hermes-timeout" value="30000" style="width:120px;" placeholder="Timeout ms"> <span style="font-size:12px;color:var(--text-muted);">ms</span>' +
                '</div>' +
            '</div>' +
            '<div class="card">' +
                '<h2>Allowed Tools (Coordinator)</h2>' +
                '<div id="hermes-tools" style="display:grid;grid-template-columns:repeat(auto-fill,minmax(180px,1fr));gap:8px;margin-top:12px;"></div>' +
            '</div>' +
            '<div style="display:flex;gap:8px;margin-top:16px;">' +
                '<button class="btn btn-primary" id="hermes-apply">Apply</button>' +
                '<button class="btn btn-secondary" id="hermes-reset">Reset</button>' +
            '</div>';

        renderTools();
        loadConfig();
        document.getElementById('hermes-apply').addEventListener('click', applyConfig);
        document.getElementById('hermes-reset').addEventListener('click', loadConfig);
    }

    function renderTools() {
        var container = document.getElementById('hermes-tools');
        if (!container) return;
        container.innerHTML = tools.map(function(t) {
            return '<label style="display:flex;align-items:center;gap:6px;font-size:12px;cursor:pointer;">' +
                '<input type="checkbox" class="hermes-tool" value="' + t + '" checked> ' + t + '</label>';
        }).join('');
    }

    function loadConfig() {
        ReefUtils.apiGet('/api/v2/hermes').then(function(data) {
            if (data.mode) {
                var radio = document.querySelector('input[name="hermes-mode"][value="' + data.mode + '"]');
                if (radio) radio.checked = true;
            }
            if (data.fallback_enabled !== undefined) {
                var fb = document.getElementById('hermes-fallback');
                if (fb) fb.checked = data.fallback_enabled;
            }
            if (data.fallback_timeout) {
                ReefUtils.setText('hermes-timeout', data.fallback_timeout);
            }
        }).catch(function() {});
    }

    function applyConfig() {
        var mode = (document.querySelector('input[name="hermes-mode"]:checked') || {}).value || 'coordinator';
        var fallback = (document.getElementById('hermes-fallback') || {}).checked || false;
        var timeout = parseInt((document.getElementById('hermes-timeout') || {}).value) || 30000;
        var allowedTools = [];
        document.querySelectorAll('.hermes-tool:checked').forEach(function(cb) { allowedTools.push(cb.value); });

        ReefUtils.apiPut('/api/v2/hermes', {
            mode: mode, fallback_enabled: fallback, fallback_timeout: timeout, allowed_tools: allowedTools
        }).then(function() {
            ReefUtils.toast('Hermes config updated', 'success');
            var badge = document.getElementById('hermes-mode-badge');
            if (badge) badge.textContent = mode.charAt(0).toUpperCase() + mode.slice(1);
        }).catch(function(e) { ReefUtils.toast('Failed: ' + e.message, 'error'); });
    }

    return { render: render };
})();
