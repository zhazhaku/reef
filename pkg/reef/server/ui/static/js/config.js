// config.js — System Configuration page
'use strict';

var ReefConfig = (function() {
    function render(container) {
        container.innerHTML = '' +
            '<div class="card">' +
                '<h2>Server Configuration</h2>' +
                '<div class="form-row" style="margin-top:12px;">' +
                    '<div class="form-group"><label>Store Type</label><select id="cfg-store-type" class="filter-select"><option value="memory">Memory</option><option value="sqlite">SQLite</option></select></div>' +
                    '<div class="form-group"><label>Store Path</label><input type="text" id="cfg-store-path" placeholder="/path/to/store"></div>' +
                '</div>' +
                '<div class="form-row">' +
                    '<div class="form-group"><label>WebSocket Address</label><input type="text" id="cfg-ws-addr" placeholder=":8080"></div>' +
                    '<div class="form-group"><label>Admin Address</label><input type="text" id="cfg-admin-addr" placeholder=":9090"></div>' +
                '</div>' +
            '</div>' +
            '<div class="card">' +
                '<h2>TLS Configuration</h2>' +
                '<div style="display:flex;align-items:center;gap:12px;margin-top:12px;">' +
                    '<label class="toggle"><input type="checkbox" id="cfg-tls-enabled"><span class="toggle-slider"></span></label>' +
                    '<span style="font-size:13px;">Enable TLS</span>' +
                '</div>' +
                '<div class="form-row" style="margin-top:12px;">' +
                    '<div class="form-group"><label>Cert File</label><input type="text" id="cfg-tls-cert" placeholder="/path/to/cert.pem"></div>' +
                    '<div class="form-group"><label>Key File</label><input type="text" id="cfg-tls-key" placeholder="/path/to/key.pem"></div>' +
                '</div>' +
            '</div>' +
            '<div class="card">' +
                '<h2>Notification Channels</h2>' +
                '<div id="cfg-notifications" style="margin-top:12px;"></div>' +
                '<button class="btn btn-secondary btn-sm" id="cfg-add-notify" style="margin-top:8px;">+ Add Channel</button>' +
            '</div>' +
            '<div style="margin-top:16px;">' +
                '<button class="btn btn-primary" id="cfg-save">Save Configuration</button>' +
            '</div>';

        loadConfig();
        document.getElementById('cfg-save').addEventListener('click', saveConfig);
        document.getElementById('cfg-add-notify').addEventListener('click', addNotifyRow);
    }

    function loadConfig() {
        ReefUtils.apiGet('/api/v2/config').then(function(data) {
            if (data.store_type) ReefUtils.setText('cfg-store-type', data.store_type);
            if (data.store_path) ReefUtils.setText('cfg-store-path', data.store_path);
            if (data.ws_addr) ReefUtils.setText('cfg-ws-addr', data.ws_addr);
            if (data.admin_addr) ReefUtils.setText('cfg-admin-addr', data.admin_addr);
            if (data.tls) {
                var tls = document.getElementById('cfg-tls-enabled');
                if (tls) tls.checked = data.tls.enabled;
                if (data.tls.cert_file) ReefUtils.setText('cfg-tls-cert', data.tls.cert_file);
                if (data.tls.key_file) ReefUtils.setText('cfg-tls-key', data.tls.key_file);
            }
            if (data.notifications) {
                var container = document.getElementById('cfg-notifications');
                if (container) {
                    container.innerHTML = '';
                    data.notifications.forEach(function(n) { addNotifyRowWithData(n); });
                }
            }
        }).catch(function() {});
    }

    function addNotifyRow() {
        addNotifyRowWithData({ type: 'webhook', url: '', token: '' });
    }

    function addNotifyRowWithData(n) {
        var container = document.getElementById('cfg-notifications');
        if (!container) return;
        var div = document.createElement('div');
        div.style.cssText = 'display:flex;gap:8px;align-items:center;margin-bottom:8px;';
        div.innerHTML =
            '<select class="filter-select notify-type" style="min-width:100px;"><option value="webhook"' + (n.type==='webhook'?' selected':'') + '>Webhook</option><option value="slack"' + (n.type==='slack'?' selected':'') + '>Slack</option><option value="feishu"' + (n.type==='feishu'?' selected':'') + '>Feishu</option><option value="wecom"' + (n.type==='wecom'?' selected':'') + '>WeCom</option></select>' +
            '<input type="url" class="notify-url" placeholder="URL" value="' + ReefUtils.escapeHtml(n.url||'') + '" style="flex:1;">' +
            '<input type="text" class="notify-token" placeholder="Token" value="' + ReefUtils.escapeHtml(n.token||'') + '" style="width:160px;">' +
            '<button class="btn btn-danger btn-sm" onclick="this.parentElement.remove()">✕</button>';
        container.appendChild(div);
    }

    function saveConfig() {
        var notifications = [];
        document.querySelectorAll('#cfg-notifications > div').forEach(function(row) {
            notifications.push({
                type: (row.querySelector('.notify-type') || {}).value || 'webhook',
                url: (row.querySelector('.notify-url') || {}).value || '',
                token: (row.querySelector('.notify-token') || {}).value || ''
            });
        });
        ReefUtils.apiPut('/api/v2/config', {
            store_type: (document.getElementById('cfg-store-type') || {}).value,
            store_path: (document.getElementById('cfg-store-path') || {}).value,
            ws_addr: (document.getElementById('cfg-ws-addr') || {}).value,
            admin_addr: (document.getElementById('cfg-admin-addr') || {}).value,
            tls: {
                enabled: (document.getElementById('cfg-tls-enabled') || {}).checked,
                cert_file: (document.getElementById('cfg-tls-cert') || {}).value,
                key_file: (document.getElementById('cfg-tls-key') || {}).value
            },
            notifications: notifications
        }).then(function() { ReefUtils.toast('Configuration saved', 'success'); })
          .catch(function(e) { ReefUtils.toast('Save failed: ' + e.message, 'error'); });
    }

    return { render: render };
})();
