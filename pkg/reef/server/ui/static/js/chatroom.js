// chatroom.js — Team Chatroom page (stub)
'use strict';

var ReefChatroom = (function() {
    var currentTaskId = '';

    function render(container, taskId) {
        currentTaskId = taskId || '';
        container.innerHTML = '' +
            '<div style="margin-bottom:12px;">' +
                '<select id="chatroom-task-select" class="filter-select" style="min-width:200px;">' +
                    '<option value="">Select a task...</option>' +
                '</select>' +
            '</div>' +
            '<div class="chatroom-layout">' +
                '<div class="chatroom-messages">' +
                    '<div class="chat-messages-list" id="chat-messages">' +
                        '<div class="empty-state"><div class="empty-icon">💬</div><div class="empty-text">Select a task to start chatting</div></div>' +
                    '</div>' +
                    '<div class="chat-input-area">' +
                        '<input type="text" id="chat-input" placeholder="Type a message..." />' +
                        '<button class="btn btn-primary" id="chat-send">Send</button>' +
                    '</div>' +
                '</div>' +
                '<div class="chatroom-sidebar">' +
                    '<div class="chatroom-agents-panel">' +
                        '<h3 style="font-size:13px;margin-bottom:8px;">Agents</h3>' +
                        '<div id="chat-agents"><div style="color:var(--text-muted);font-size:12px;">No agents active</div></div>' +
                    '</div>' +
                    '<div class="chatroom-task-tree">' +
                        '<h3 style="font-size:13px;margin-bottom:8px;">Task Tree</h3>' +
                        '<div id="chat-task-tree"><div style="color:var(--text-muted);font-size:12px;">No sub-tasks</div></div>' +
                    '</div>' +
                '</div>' +
            '</div>';

        loadTaskList();
        setupChatInput();
        if (currentTaskId) loadMessages(currentTaskId);
    }

    function loadTaskList() {
        ReefUtils.apiGet('/api/v2/tasks?limit=50&status=Running').then(function(data) {
            var sel = document.getElementById('chatroom-task-select');
            if (!sel) return;
            (data.tasks || []).forEach(function(t) {
                var opt = document.createElement('option');
                opt.value = t.id;
                opt.textContent = t.id + ': ' + ReefUtils.truncate(t.instruction, 40);
                if (t.id === currentTaskId) opt.selected = true;
                sel.appendChild(opt);
            });
            sel.addEventListener('change', function() {
                currentTaskId = sel.value;
                if (currentTaskId) loadMessages(currentTaskId);
            });
        }).catch(function() {});
    }

    function loadMessages(taskId) {
        ReefUtils.apiGet('/api/v2/chatroom/' + taskId).then(function(data) {
            var list = document.getElementById('chat-messages');
            if (!list) return;
            list.innerHTML = '';
            if (!data.messages || data.messages.length === 0) {
                list.innerHTML = '<div class="empty-state"><div class="empty-icon">💬</div><div class="empty-text">No messages yet</div></div>';
                return;
            }
            data.messages.forEach(function(m) { appendMessage(list, m); });
            list.scrollTop = list.scrollHeight;
        }).catch(function() {});
    }

    function appendMessage(list, m) {
        var isUser = m.sender_type === 'user';
        var div = document.createElement('div');
        div.className = 'chat-message' + (isUser ? ' user' : '');
        var contentClass = '';
        if (m.content_type === 'reasoning') contentClass = ' chat-message-type-reasoning';
        else if (m.content_type === 'tool_call') contentClass = ' chat-message-type-tool_call';
        div.innerHTML =
            '<div class="chat-message-avatar">' + ReefUtils.escapeHtml((m.sender || '?')[0].toUpperCase()) + '</div>' +
            '<div class="chat-message-body">' +
                '<div class="chat-message-sender">' + ReefUtils.escapeHtml(m.sender) + '</div>' +
                '<div class="' + contentClass + '">' + ReefUtils.escapeHtml(m.content) + '</div>' +
                '<div class="chat-message-time">' + ReefUtils.formatTime(m.timestamp) + '</div>' +
            '</div>';
        list.appendChild(div);
    }

    function setupChatInput() {
        var input = document.getElementById('chat-input');
        var btn = document.getElementById('chat-send');
        if (!input || !btn) return;
        function send() {
            var msg = input.value.trim();
            if (!msg || !currentTaskId) return;
            ReefUtils.apiPost('/api/v2/chatroom/' + currentTaskId + '/send', { content: msg })
                .then(function() {
                    input.value = '';
                    loadMessages(currentTaskId);
                })
                .catch(function(err) { ReefUtils.toast('Send failed: ' + err.message, 'error'); });
        }
        btn.addEventListener('click', send);
        input.addEventListener('keydown', function(e) { if (e.key === 'Enter') send(); });
    }

    function onMessage(data) {
        if (!data || !currentTaskId) return;
        var list = document.getElementById('chat-messages');
        if (list) {
            appendMessage(list, data);
            list.scrollTop = list.scrollHeight;
        }
    }

    return { render: render, onMessage: onMessage };
})();
