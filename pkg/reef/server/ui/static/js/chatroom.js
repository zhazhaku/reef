// chatroom.js — Team Chatroom with task selector, multi-type messages, agent panel, task tree
'use strict';

var ReefChatroom = (function() {
    var currentTaskId = '';
    var messageOffset = 0;
    var messageLimit = 50;
    var hasMore = false;
    var sessionEventSource = null;
    var agentColors = {};

    // ---- Color palette for avatars ----
    var AVATAR_COLORS = [
        '#e94560', '#2196f3', '#4caf50', '#ff9800', '#9c27b0',
        '#00bcd4', '#8bc34a', '#ff5722', '#3f51b5', '#009688',
        '#e91e63', '#03a9f4', '#cddc39', '#ffc107', '#795548'
    ];

    function getAvatarColor(name) {
        if (!name) return AVATAR_COLORS[0];
        if (agentColors[name]) return agentColors[name];
        var idx = 0;
        for (var i = 0; i < name.length; i++) { idx = (idx * 31 + name.charCodeAt(i)) % AVATAR_COLORS.length; }
        agentColors[name] = AVATAR_COLORS[idx];
        return agentColors[name];
    }

    // ---- Render ----
    function render(container, taskId) {
        currentTaskId = taskId || '';
        messageOffset = 0;
        hasMore = false;
        closeSessionStream();

        container.innerHTML =
            '<div style="margin-bottom:12px;display:flex;gap:8px;align-items:center;">' +
                '<select id="chatroom-task-select" class="filter-select" style="min-width:280px;">' +
                    '<option value="">-- Select a task to start chatting --</option>' +
                '</select>' +
                '<span id="chatroom-task-status" style="font-size:11px;color:var(--text-muted);"></span>' +
            '</div>' +
            '<div class="chatroom-layout">' +
                '<div class="chatroom-messages">' +
                    '<div class="chat-messages-list" id="chat-messages">' +
                        '<div class="empty-state" id="chat-empty-state">' +
                            '<div class="empty-icon">💬</div>' +
                            '<div class="empty-text">Select a task to start chatting</div>' +
                        '</div>' +
                    '</div>' +
                    '<div class="chat-input-area">' +
                        '<input type="text" id="chat-input" placeholder="Type a message..." disabled />' +
                        '<button class="btn btn-primary" id="chat-send" disabled>Send</button>' +
                    '</div>' +
                '</div>' +
                '<div class="chatroom-sidebar">' +
                    '<div class="chatroom-agents-panel">' +
                        '<h3 style="font-size:13px;margin-bottom:8px;">🖥 Agents</h3>' +
                        '<div id="chat-agents"><div style="color:var(--text-muted);font-size:12px;">No task selected</div></div>' +
                    '</div>' +
                    '<div class="chatroom-task-tree">' +
                        '<h3 style="font-size:13px;margin-bottom:8px;">🌳 Task Tree</h3>' +
                        '<div id="chat-task-tree"><div style="color:var(--text-muted);font-size:12px;">No task selected</div></div>' +
                    '</div>' +
                '</div>' +
            '</div>';

        loadTaskList();
        setupChatInput();
        if (currentTaskId) {
            selectTask(currentTaskId);
        }
    }

    // ---- Task selector ----
    function loadTaskList() {
        ReefUtils.apiGet('/api/v2/tasks?limit=100&status=Running').then(function(data) {
            var sel = document.getElementById('chatroom-task-select');
            if (!sel) return;
            var tasks = data.tasks || [];
            // Keep first placeholder option
            while (sel.options.length > 1) sel.remove(1);
            tasks.forEach(function(t) {
                var opt = document.createElement('option');
                opt.value = t.id;
                opt.textContent = t.id.substring(0, 8) + ' — ' + ReefUtils.truncate(t.instruction || '(no instruction)', 50);
                if (t.id === currentTaskId) opt.selected = true;
                sel.appendChild(opt);
            });
            if (currentTaskId && !tasks.some(function(t) { return t.id === currentTaskId; })) {
                // Current task is not running — add it anyway
                var opt = document.createElement('option');
                opt.value = currentTaskId;
                opt.textContent = currentTaskId.substring(0, 8) + ' (selected)';
                opt.selected = true;
                sel.appendChild(opt);
            }
            sel.addEventListener('change', function() {
                var newId = sel.value;
                if (newId !== currentTaskId) {
                    selectTask(newId);
                }
            });
        }).catch(function(err) {
            console.error('Failed to load task list:', err);
        });
    }

    function selectTask(taskId) {
        currentTaskId = taskId;
        messageOffset = 0;
        hasMore = false;

        var input = document.getElementById('chat-input');
        var btn = document.getElementById('chat-send');
        var statusEl = document.getElementById('chatroom-task-status');
        var emptyState = document.getElementById('chat-empty-state');
        var list = document.getElementById('chat-messages');

        if (!taskId) {
            if (input) input.disabled = true;
            if (btn) btn.disabled = true;
            if (statusEl) statusEl.textContent = '';
            if (list) list.innerHTML = '';
            if (emptyState) {
                emptyState.style.display = '';
                emptyState.querySelector('.empty-icon').textContent = '💬';
                emptyState.querySelector('.empty-text').textContent = 'Select a task to start chatting';
            }
            document.getElementById('chat-agents').innerHTML = '<div style="color:var(--text-muted);font-size:12px;">No task selected</div>';
            document.getElementById('chat-task-tree').innerHTML = '<div style="color:var(--text-muted);font-size:12px;">No task selected</div>';
            return;
        }

        if (input) input.disabled = false;
        if (btn) btn.disabled = false;
        if (statusEl) statusEl.textContent = 'Task: ' + taskId.substring(0, 8);
        if (list) list.innerHTML = '';
        if (emptyState) emptyState.style.display = 'none';

        loadMessages(taskId);
        loadAgents(taskId);
        loadTaskTree(taskId);
    }

    // ---- Messages ----
    function loadMessages(taskId) {
        if (!taskId) return;
        var url = '/api/v2/chatroom/' + taskId + '?limit=' + messageLimit;
        if (messageOffset > 0) url += '&offset=' + messageOffset;

        ReefUtils.apiGet(url).then(function(data) {
            var list = document.getElementById('chat-messages');
            if (!list || currentTaskId !== taskId) return;

            var messages = data.messages || [];
            var total = data.total || messages.length;
            hasMore = (messageOffset + messages.length) < total;

            // Clear list on first load, prepend on load-more
            if (messageOffset === 0) {
                list.innerHTML = '';
            }

            // Remove load-more button if present
            var loadMoreBtn = document.getElementById('chat-load-more');
            if (loadMoreBtn) loadMoreBtn.remove();

            if (messages.length === 0 && messageOffset === 0) {
                var emptyState = document.getElementById('chat-empty-state');
                if (emptyState) {
                    emptyState.style.display = '';
                    emptyState.querySelector('.empty-icon').textContent = '💬';
                    emptyState.querySelector('.empty-text').textContent = 'No messages yet. Start the conversation!';
                }
                return;
            }

            // Hide empty state
            var emptyState = document.getElementById('chat-empty-state');
            if (emptyState) emptyState.style.display = 'none';

            // Prepend messages (they come in reverse chronological order from API when using offset)
            // Actually, let's assume /api/v2/chatroom returns messages in chronological order
            // For "load more", we prepend older messages at the top
            if (messageOffset > 0) {
                // Insert at top, preserving scroll position
                var prevScrollHeight = list.scrollHeight;
                var frag = document.createDocumentFragment();
                messages.forEach(function(m) { frag.appendChild(renderMessage(m)); });
                list.insertBefore(frag, list.firstChild);
                list.scrollTop = list.scrollHeight - prevScrollHeight;
            } else {
                messages.forEach(function(m) { list.appendChild(renderMessage(m)); });
                list.scrollTop = list.scrollHeight;
            }

            // Add "load more" button at top if there are more messages
            if (hasMore) {
                var btn = document.createElement('button');
                btn.id = 'chat-load-more';
                btn.className = 'btn btn-secondary btn-sm';
                btn.textContent = '↑ Load older messages';
                btn.style.cssText = 'width:100%;margin-bottom:8px;';
                btn.addEventListener('click', function() {
                    messageOffset += messageLimit;
                    loadMessages(currentTaskId);
                });
                list.insertBefore(btn, list.firstChild);
            }
        }).catch(function(err) {
            console.error('Failed to load messages:', err);
            var list = document.getElementById('chat-messages');
            if (list && messageOffset === 0) {
                var emptyState = document.getElementById('chat-empty-state');
                if (emptyState) {
                    emptyState.style.display = '';
                    emptyState.querySelector('.empty-icon').textContent = '⚠️';
                    emptyState.querySelector('.empty-text').textContent = 'Failed to load messages';
                }
            }
        });
    }

    function renderMessage(m) {
        var contentType = (m.content_type || 'text').toLowerCase();
        var senderType = (m.sender_type || 'agent').toLowerCase();
        var sender = m.sender || 'System';
        var timestamp = m.timestamp || m.created_at || new Date().toISOString();
        var content = m.content || '';

        // --- System messages ---
        if (contentType === 'system') {
            var sysDiv = document.createElement('div');
            sysDiv.className = 'chat-message chat-message-system';
            sysDiv.style.cssText = 'justify-content:center;max-width:100%;';
            sysDiv.innerHTML =
                '<div style="font-size:11px;color:var(--text-muted);text-align:center;padding:4px 12px;">' +
                    '<span>' + ReefUtils.escapeHtml(content) + '</span>' +
                    '<span style="margin-left:8px;font-size:10px;">' + formatMessageTime(timestamp) + '</span>' +
                '</div>';
            return sysDiv;
        }

        // --- Regular message ---
        var isUser = senderType === 'user';
        var div = document.createElement('div');
        div.className = 'chat-message' + (isUser ? ' user' : '');

        // Avatar
        var avatarLetter = (sender || '?')[0].toUpperCase();
        var avatarColor = getAvatarColor(sender);
        var avatarHtml =
            '<div class="chat-message-avatar" style="background:' + avatarColor + ';color:white;">' +
                avatarLetter +
            '</div>';

        // Body
        var bodyClass = '';
        var bodyStyle = '';
        var bodyContent = '';

        switch (contentType) {
            case 'reasoning':
                bodyClass = 'chat-message-type-reasoning';
                bodyContent = ReefUtils.escapeHtml(content);
                break;
            case 'tool_call':
                bodyClass = 'chat-message-type-tool_call';
                bodyContent = formatToolCall(content, m.tool_name, m.tool_args);
                break;
            case 'exec_result':
                bodyClass = 'chat-message-type-exec_result';
                bodyStyle = 'border-left:3px solid var(--success);font-family:monospace;font-size:12px;background:var(--bg-primary);padding:6px 8px;border-radius:var(--radius-sm);white-space:pre-wrap;word-break:break-all;';
                bodyContent = ReefUtils.escapeHtml(content);
                break;
            default: // text
                bodyClass = '';
                bodyContent = ReefUtils.escapeHtml(content);
                break;
        }

        div.innerHTML =
            avatarHtml +
            '<div class="chat-message-body">' +
                '<div class="chat-message-sender">' + ReefUtils.escapeHtml(sender) + '</div>' +
                '<div class="' + bodyClass + '" style="' + bodyStyle + '">' + bodyContent + '</div>' +
                '<div class="chat-message-time">' + formatMessageTime(timestamp) + '</div>' +
            '</div>';

        return div;
    }

    function formatToolCall(rawContent, toolName, toolArgs) {
        // If we have structured tool_name/tool_args, format nicely
        if (toolName) {
            var argsStr = '';
            try {
                argsStr = typeof toolArgs === 'string' ? toolArgs : JSON.stringify(toolArgs, null, 2);
            } catch (e) {
                argsStr = String(toolArgs || '');
            }
            return '<div style="color:var(--accent);font-weight:600;margin-bottom:4px;">🔧 ' + ReefUtils.escapeHtml(toolName) + '</div>' +
                   '<pre style="margin:0;font-size:11px;color:var(--text-secondary);">' + ReefUtils.escapeHtml(argsStr) + '</pre>';
        }
        // Otherwise just display raw content as monospace
        return '<pre style="margin:0;font-size:12px;">' + ReefUtils.escapeHtml(rawContent) + '</pre>';
    }

    function formatMessageTime(ts) {
        if (!ts) return '';
        try {
            var d = new Date(ts);
            if (isNaN(d.getTime())) return '';
            var h = d.getHours();
            var m = d.getMinutes();
            return (h < 10 ? '0' : '') + h + ':' + (m < 10 ? '0' : '') + m;
        } catch (e) {
            return '';
        }
    }

    // ---- Chat input ----
    function setupChatInput() {
        var input = document.getElementById('chat-input');
        var btn = document.getElementById('chat-send');
        if (!input || !btn) return;

        function send() {
            var msg = input.value.trim();
            if (!msg || !currentTaskId) return;

            // Optimistic UI: show user message immediately
            var list = document.getElementById('chat-messages');
            var emptyState = document.getElementById('chat-empty-state');
            if (emptyState) emptyState.style.display = 'none';

            var tempMsg = {
                content_type: 'text',
                sender_type: 'user',
                sender: 'You',
                content: msg,
                timestamp: new Date().toISOString()
            };
            if (list) {
                list.appendChild(renderMessage(tempMsg));
                list.scrollTop = list.scrollHeight;
            }

            input.value = '';
            input.disabled = true;
            btn.disabled = true;

            ReefUtils.apiPost('/api/v2/chatroom/' + currentTaskId + '/send', { content: msg })
                .then(function() {
                    input.disabled = false;
                    btn.disabled = false;
                    input.focus();
                    // Reload to get the actual message from server
                    messageOffset = 0;
                    hasMore = false;
                    loadMessages(currentTaskId);
                })
                .catch(function(err) {
                    ReefUtils.toast('Send failed: ' + err.message, 'error');
                    input.disabled = false;
                    btn.disabled = false;
                    input.focus();
                });
        }

        // Remove old listeners by cloning
        var newBtn = btn.cloneNode(true);
        btn.parentNode.replaceChild(newBtn, btn);
        var newInput = input.cloneNode(true);
        input.parentNode.replaceChild(newInput, input);

        newBtn.addEventListener('click', send);
        newInput.addEventListener('keydown', function(e) {
            if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault();
                send();
            }
        });
    }

    // ---- Agents panel ----
    function loadAgents(taskId) {
        if (!taskId) return;
        var panel = document.getElementById('chat-agents');
        if (!panel) return;

        // Fetch task details to find assigned client, then fetch all clients
        Promise.all([
            ReefUtils.apiGet('/api/v2/tasks/' + taskId).catch(function() { return null; }),
            ReefUtils.apiGet('/api/v2/clients').catch(function() { return []; })
        ]).then(function(results) {
            if (currentTaskId !== taskId) return;
            var task = results[0];
            var clients = results[1] || [];
            var assignedId = task ? task.assigned_client : null;

            if (!clients.length) {
                panel.innerHTML = '<div style="color:var(--text-muted);font-size:12px;">No agents connected</div>';
                return;
            }

            // Show assigned agent first, then others
            var assigned = [];
            var others = [];
            clients.forEach(function(c) {
                if (c.id === assignedId) assigned.push(c);
                else others.push(c);
            });

            var html = '';
            var allAgents = assigned.concat(others);
            allAgents.forEach(function(c) {
                var avatarColor = getAvatarColor(c.id);
                var stateClass = ReefUtils.stateClass(c.state || 'offline');
                html +=
                    '<div style="display:flex;align-items:center;gap:8px;padding:6px 0;border-bottom:1px solid var(--border-light);">' +
                        '<div style="width:24px;height:24px;border-radius:50%;background:' + avatarColor +
                            ';color:white;display:flex;align-items:center;justify-content:center;font-size:10px;font-weight:700;flex-shrink:0;position:relative;">' +
                            (c.id || '?')[0].toUpperCase() +
                            '<span class="status-dot ' + stateClass + '" style="position:absolute;bottom:-2px;right:-2px;width:8px;height:8px;border:2px solid var(--bg-card);"></span>' +
                        '</div>' +
                        '<div style="flex:1;min-width:0;">' +
                            '<div style="font-size:12px;font-weight:600;">' + ReefUtils.escapeHtml(c.id) +
                                (c.id === assignedId ? ' <span style="color:var(--accent);font-size:10px;">(assigned)</span>' : '') +
                            '</div>' +
                            '<div style="font-size:10px;color:var(--text-muted);">' + ReefUtils.escapeHtml(c.role || '') + '</div>' +
                        '</div>' +
                    '</div>';
            });

            panel.innerHTML = html || '<div style="color:var(--text-muted);font-size:12px;">No agents connected</div>';
        }).catch(function() {
            panel.innerHTML = '<div style="color:var(--text-muted);font-size:12px;">Failed to load agents</div>';
        });
    }

    // ---- Task tree panel ----
    function loadTaskTree(taskId) {
        if (!taskId) return;
        var treePanel = document.getElementById('chat-task-tree');
        if (!treePanel) return;

        ReefUtils.apiGet('/api/v2/tasks/' + taskId + '/decompose').then(function(data) {
            if (currentTaskId !== taskId) return;
            var nodes = data.sub_tasks || data.children || [];
            if (!nodes.length) {
                treePanel.innerHTML = '<div style="color:var(--text-muted);font-size:12px;">No sub-tasks</div>';
                return;
            }
            treePanel.innerHTML = '';
            nodes.forEach(function(n) { renderTaskTreeNode(treePanel, n, 0); });
        }).catch(function(err) {
            if (currentTaskId === taskId) {
                treePanel.innerHTML = '<div style="color:var(--text-muted);font-size:12px;">Failed to load task tree</div>';
            }
        });
    }

    function renderTaskTreeNode(parent, node, depth) {
        var statusIcons = {
            'completed': '✅', 'done': '✅', 'running': '🟡', 'in_progress': '🟡',
            'queued': '⏳', 'blocked': '🔴', 'failed': '❌', 'cancelled': '🚫'
        };
        var icon = statusIcons[(node.status || '').toLowerCase()] || '⏳';
        var div = document.createElement('div');
        div.style.cssText =
            'padding:4px 0 4px ' + (depth * 12 + 4) + 'px;' +
            'font-size:11px;display:flex;align-items:flex-start;gap:4px;' +
            'border-left:' + (depth > 0 ? '1px solid var(--border)' : 'none') + ';' +
            'margin-left:' + (depth > 0 ? '6px' : '0') + ';';
        div.innerHTML =
            '<span style="flex-shrink:0;">' + icon + '</span>' +
            '<span style="color:var(--text-secondary);line-height:1.4;">' +
                ReefUtils.escapeHtml(ReefUtils.truncate(node.instruction || node.text || '(unnamed)', 60)) +
            '</span>';
        parent.appendChild(div);
        (node.children || []).forEach(function(c) { renderTaskTreeNode(parent, c, depth + 1); });
    }

    // ---- SSE ----
    function onMessage(data) {
        if (!data || !currentTaskId) return;
        // Check if the message belongs to the current task
        if (data.task_id && data.task_id !== currentTaskId) return;

        var list = document.getElementById('chat-messages');
        if (!list) return;

        // Hide empty state
        var emptyState = document.getElementById('chat-empty-state');
        if (emptyState) emptyState.style.display = 'none';

        list.appendChild(renderMessage(data));
        // Auto-scroll to bottom
        list.scrollTop = list.scrollHeight;
    }

    // ---- Session stream for client detail (not used directly, but for SSE event forwarding) ----
    function openSessionStream(clientId, targetEl) {
        closeSessionStream();
        if (!clientId || !targetEl) return;

        sessionEventSource = new EventSource('/api/v2/client/' + clientId + '/session');
        sessionEventSource.onmessage = function(e) {
            try {
                var data = JSON.parse(e.data);
                var line = document.createElement('div');
                line.style.cssText = 'padding:2px 0;border-bottom:1px solid var(--border-light);font-size:11px;';
                var ts = data.timestamp ? formatMessageTime(data.timestamp) + ' ' : '';
                line.textContent = ts + (data.event || data.type || 'event') + ': ' + (data.message || JSON.stringify(data));
                targetEl.appendChild(line);
                targetEl.scrollTop = targetEl.scrollHeight;
            } catch (err) {
                var errLine = document.createElement('div');
                errLine.style.cssText = 'padding:2px 0;font-size:11px;color:var(--text-muted);';
                errLine.textContent = e.data;
                targetEl.appendChild(errLine);
                targetEl.scrollTop = targetEl.scrollHeight;
            }
        };
        sessionEventSource.onerror = function() {
            var errLine = document.createElement('div');
            errLine.style.cssText = 'padding:2px 0;font-size:11px;color:var(--error);';
            errLine.textContent = '[Session stream disconnected]';
            targetEl.appendChild(errLine);
            closeSessionStream();
        };
    }

    function closeSessionStream() {
        if (sessionEventSource) {
            sessionEventSource.close();
            sessionEventSource = null;
        }
    }

    // ---- Cleanup ----
    function cleanup() {
        closeSessionStream();
    }

    return {
        render: render,
        onMessage: onMessage,
        openSessionStream: openSessionStream,
        closeSessionStream: closeSessionStream,
        cleanup: cleanup,
        refresh: function() {
            if (currentTaskId) {
                loadMessages(currentTaskId);
                loadAgents(currentTaskId);
                loadTaskTree(currentTaskId);
            }
        }
    };
})();
